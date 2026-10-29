"""VM worlds, checked without a VM.

A fake lab stands in for netsim, ssh and the guests, and records every call
in order. The real runner drives the real VmWorld against it, so these
assertions pin the netsim call sequence a run makes — boot, push, tunnels,
ops, logs, kill — and what a run records, without booting anything.
"""
import argparse
import contextlib
import io
import json
import os
import tempfile
import unittest
from pathlib import Path
from types import SimpleNamespace
from unittest import mock

from lib import runner
from lib.executors import NODE_FIELDS, ExecutorError
from lib.worlds import vm
from lib.worlds.vm import VmWorld, recipe_hash

TESTS = Path(__file__).resolve().parent.parent
VMS = ["node1", "node2", "reflector"]
LAB = f"e2e-lab-r{recipe_hash(vm.RECIPE)}"


class FakeProc:
    """An `ssh -L` that stays up until terminated, which it records."""

    terminated = False

    def __init__(self, calls: list, tunnel: tuple):
        self.calls, self.tunnel = calls, tunnel

    def poll(self):
        return None

    def terminate(self):
        self.terminated = True
        self.calls.append(("terminate", *self.tunnel))


class FakeLab:
    """netsim and its guests: stages, simulations, and what the guests hold.

    Every call lands in `calls`, in order: ("netsim", *argv), ("scp", sim,
    vm, dst), ("tunnel", sim, vm, target), ("terminate", sim, vm, target),
    ("op", sim, label, argv, ids). A guest that ran enter-nat answers
    `priv` when asked for its network namespaces, as a NAT'd one does.
    """

    def __init__(self, home: Path, stages=("null",), running=()):
        self.home = home
        self.calls = []
        self.stages = set(stages)
        self.sims = {sim: None for sim in running}     # id -> stage booted
        self.next_id = 7
        self.procs = []
        self.natted = set()     # (sim, vm) that ran enter-nat
        # the identity each guest's apphost answers with, by tunnel port
        self.identity = lambda port: f"id-{port}"

    # -- netsim ------------------------------------------------------------

    def netsim(self, *args, check=True, capture=True) -> str:
        args = tuple(str(a) for a in args)
        self.calls.append(("netsim", *args))
        verb = args[0]
        if args[:2] == ("stages", "--json"):
            return json.dumps([{"slug": s} for s in sorted(self.stages)])
        if args[:2] == ("ps", "--json"):
            return json.dumps([{"sim_id": s, "vms": VMS} for s in self.sims])
        if verb == "story":
            self.stages.add(args[args.index("--save") + 1])
        elif verb == "task":
            self.boot(args[args.index("--stage") + 1])
            self.stages.add(args[args.index("--save") + 1])
        elif verb == "remove":
            self.stages.discard(args[1])
        elif args[:1] == ("kill",) and len(args) == 2:
            self.sims.pop(args[1], None)
        elif verb == "ssh":
            return self.guest(args[2], args[3], args[5])
        return ""

    def boot(self, stage: str) -> None:
        sim = str(self.next_id)
        self.next_id += 1
        self.sims[sim] = stage
        d = self.home / "sims" / sim
        d.mkdir(parents=True)
        (d / "manifest.json").write_text(json.dumps({"vms": [
            {"hostname": h, "ip": f"10.77.0.{10 + i}", "state": "running"}
            for i, h in enumerate(VMS)]}))

    def guest(self, sim: str, host: str, script: str) -> str:
        if script.startswith("ip netns list"):
            return "priv\n" if (sim, host) in self.natted else ""
        if script.startswith("journalctl"):
            return f"journal of {host} in {sim}"
        return ""

    # -- the VmWorld methods that leave the host -----------------------------

    def scp(self, world, host, src, dst):
        self.calls.append(("scp", world.sim, host, dst))

    def tunnel(self, world, host, target=None):
        self.calls.append(("tunnel", world.sim, host, target))
        self.procs.append(FakeProc(self.calls, (world.sim, host, target)))
        world._tunnels.append(self.procs[-1])     # as the real one does
        return 30000 + len(self.procs), self.procs[-1]

    def vmop(self, world, script, argv, label):
        env = world._op_env()
        ids = {k: v for k, v in env.items() if k.startswith("ASTRAL_ID_")}
        assert env["NETSIM_SIM_DIR"] == str(self.home / "sims" / world.sim)
        self.calls.append(("op", world.sim, label, tuple(argv), ids))
        if label == "enter-nat":
            self.natted |= {(world.sim, a) for a in argv if a != "--vm"}

    def patches(self) -> list:
        # note: plain functions, so each binds to the world it is called on
        return [mock.patch.object(vm, "_netsim", self.netsim),
                mock.patch.object(VmWorld, "_scp",
                                  lambda w, *a: self.scp(w, *a)),
                mock.patch.object(VmWorld, "_tunnel",
                                  lambda w, *a: self.tunnel(w, *a)),
                mock.patch.object(VmWorld, "_vmop",
                                  lambda w, *a: self.vmop(w, *a)),
                mock.patch.object(VmWorld, "_whoami",
                                  lambda w, port, token: self.identity(port)),
                # why no grace: the fake reaps a killed simulation at once,
                # and one still listed fails its close straight away
                mock.patch.object(vm, "KILL_TIMEOUT", 0.0),
                mock.patch.dict(os.environ, {"NETSIM_HOME": str(self.home)})]

    def netsim_verbs(self, sim=None) -> list:
        """The netsim verbs called, `ssh` folded into what it did, and the
        ops run, in order."""
        out = []
        for c in self.calls:
            if c[0] == "op" and (sim is None or c[1] == sim):
                out.append(("op", c[2]))
            if c[0] != "netsim":
                continue
            if c[1] == "ssh":
                if sim is None or c[3] == sim:
                    out.append(("ssh", c[4], c[6].split()[0]))
            elif sim is None or sim in c:
                out.append(c[1:3] if c[1] in ("stages", "ps") else c[1:2])
        return out


def run(tmp: Path, lab: FakeLab, selection: list, **opts) -> tuple:
    """(exit code, results doc, run dir, the tests whose steps ran) of one
    runner.main over the fake lab, its results under tmp."""
    args = argparse.Namespace(**{
        "selection": selection, "driver": "script", "target": "fresh",
        "keep": False, "plan": False, "isolate": False, "prereqs": "script",
        "vm": False, **opts})
    ran = []

    def step(py, script, log, env, timeout):
        ran.append((script.parent.name, script.stem))
        return 0

    with contextlib.ExitStack() as stack:
        for p in lab.patches():
            stack.enter_context(p)
        for p in (mock.patch.object(runner, "RESULTS", tmp / "results"),
                  mock.patch.object(runner, "ensure_binary",
                                    return_value=(tmp / "astrald", "abc1234")),
                  mock.patch.object(runner, "lease_ports",
                                    return_value=SimpleNamespace(base=0)),
                  mock.patch.object(runner, "_run_step", step),
                  contextlib.redirect_stdout(io.StringIO()),
                  contextlib.redirect_stderr(io.StringIO())):
            stack.enter_context(p)
        code = runner.main(args)
    dirs = sorted((tmp / "results").iterdir()) if (tmp / "results").exists() \
        else []
    if not dirs:
        return code, None, None, ran
    doc = json.loads((dirs[-1] / "results.json").read_text())
    return code, doc, dirs[-1], ran


class Lab(unittest.TestCase):
    def setUp(self):
        t = tempfile.TemporaryDirectory()
        self.addCleanup(t.cleanup)
        self.tmp = Path(t.name)

    def lab(self, **kw) -> FakeLab:
        return FakeLab(self.tmp / "netsim", **kw)


class TestFreshVmWorlds(Lab):
    """`tor-link nat-punch`: two VM worlds, each built from the lab."""

    def setUp(self):
        super().setUp()
        self.fake = self.lab()
        self.code, self.doc, self.run_dir, self.ran = run(
            self.tmp, self.fake, ["tor-link", "nat-punch"])

    def test_two_vm_worlds_replay_their_prereqs_and_pass(self):
        self.assertEqual(self.code, 0)
        self.assertEqual(
            [(e["world"], e["test"], e["kind"], e["env"], e["artifacts"])
             for e in self.doc["entries"]],
            [("w0", "bootstrap-user-software-key", "prereq", "netsim",
              "bootstrap-user-software-key/"),
             ("w0", "adopt-node", "prereq", "netsim", "adopt-node/"),
             ("w0", "nat-punch", "test", "netsim", "nat-punch/"),
             ("w1", "bootstrap-user-software-key", "prereq", "netsim",
              "bootstrap-user-software-key.w1/"),
             ("w1", "adopt-node", "prereq", "netsim", "adopt-node.w1/"),
             ("w1", "tor-link", "test", "netsim", "tor-link/")])
        self.assertEqual(self.doc["pushed"], "node1,node2,reflector")

    def test_each_world_boots_the_lab_once_and_dies_before_the_next(self):
        # why: one VM world at a time — three 2 GiB guests each
        def boot(bake):
            return [("ps", "--json"), ("stages", "--json"),
                    *([("story",)] if bake else []),
                    ("task",), ("ps", "--json"), ("remove",),
                    *[("ssh", h, "set") for h in VMS]]     # the push, per VM

        def step(*roster, ops=()):
            # the session opens before the ops and again after them, each
            # time asking where apphost listens; then the world's pulse
            probe = [("ssh", h, "ip") for h in roster]
            return (probe + [("op", op) for op in ops] + probe
                    + [("ps", "--json")])

        close = [*[("ssh", h, "journalctl") for h in VMS], ("kill",),
                 ("ps", "--json")]            # gone before the next boot
        pair = ("node1", "node2")
        nat = ("enable-tor", "enable-tor:verify.sh", "enter-nat",
               "expose-apphost", "configure-nat-tor", "add-reflector",
               "add-reflector:verify.sh")
        tor = ("enable-tor", "enable-tor:verify.sh", "leave-lan",
               "leave-lan:verify.sh")
        self.assertEqual(self.fake.netsim_verbs(), [
            *boot(bake=True), *step("node1"), *step(*pair),
            *step(*pair, ops=nat), *close,
            *boot(bake=False), *step("node1"), *step(*pair),
            *step(*pair, ops=tor), *close])
        task = ("netsim", "task", "--stage", LAB, "--keep", "--save",
                vm.SCRATCH_STAGE, "noop")
        self.assertEqual([c for c in self.fake.calls if c[:2] == task[:2]],
                         [task, task])
        self.assertEqual([c for c in self.fake.calls
                          if c[:2] == ("netsim", "kill")],
                         [("netsim", "kill", "7"), ("netsim", "kill", "8")])
        boots = [c for c in self.fake.calls if c[:2] == ("netsim", "task")]
        self.assertEqual([c[c.index("--stage") + 1] for c in boots],
                         [LAB, LAB])
        self.assertEqual(self.fake.sims, {})        # both killed
        self.assertTrue(all(p.terminated for p in self.fake.procs))

    def test_no_state_is_cached_between_worlds(self):
        # why: the per-state stage cache saved after every producer and
        # booted only the first world; no run writes a stage now
        self.assertFalse([c for c in self.fake.calls
                          if c[:2] == ("netsim", "save")])
        self.assertEqual(self.fake.stages, {"null", LAB})

    def test_the_binaries_reach_every_vm_of_every_world(self):
        scps = [c[1:] for c in self.fake.calls if c[0] == "scp"]
        self.assertEqual(scps, [
            (sim, h, dst) for sim in ("7", "8") for h in VMS
            for dst in ("/usr/local/bin/astrald",
                        "/usr/local/bin/astral-query")])

    def test_ops_run_with_their_verifiers_in_their_own_world(self):
        ops = [(c[1], c[2], c[3]) for c in self.fake.calls if c[0] == "op"]
        both = ("--vm", "node1", "--vm", "node2")
        reflect = both + ("--reflector", "reflector")
        lan = ("--vm", "node2", "--peer", "node1")
        # note: an op's verify.sh wins over its verify.py, as it always has
        self.assertEqual(ops, [
            ("7", "enable-tor", both), ("7", "enable-tor:verify.sh", both),
            ("7", "enter-nat", both), ("7", "expose-apphost", both),
            ("7", "configure-nat-tor", both),
            ("7", "add-reflector", reflect),
            ("7", "add-reflector:verify.sh", reflect),
            ("8", "enable-tor", both), ("8", "enable-tor:verify.sh", both),
            ("8", "leave-lan", lan), ("8", "leave-lan:verify.sh", lan)])
        # an op learns each roster node's identity from its tunnel
        ids = [c[4] for c in self.fake.calls if c[0] == "op"]
        self.assertTrue(all(set(i) == {"ASTRAL_ID_node1", "ASTRAL_ID_node2"}
                            for i in ids))

    def test_enter_nat_moves_the_tunnels_to_the_private_address(self):
        # why: enter-nat puts apphost in netns priv, and the forward that
        # still reaches 127.0.0.1 forwards to nothing while its ssh lives on
        moved = [c[2:] for c in self.fake.calls
                 if c[0] in ("tunnel", "terminate") and c[1] == "7"
                 and c[2] in ("node1", "node2")]
        self.assertEqual(moved, [
            ("node1", "127.0.0.1"), ("node2", "127.0.0.1"),
            ("node1", "127.0.0.1"), ("node1", vm.NAT_NETNS_HOST),
            ("node2", "127.0.0.1"), ("node2", vm.NAT_NETNS_HOST),
            ("node1", vm.NAT_NETNS_HOST), ("node2", vm.NAT_NETNS_HOST)])
        kinds = [c[0] for c in self.fake.calls
                 if c[0] in ("tunnel", "terminate") and c[1] == "7"
                 and c[2] in ("node1", "node2")]
        self.assertEqual(kinds, ["tunnel", "tunnel", "terminate", "tunnel",
                                 "terminate", "tunnel", "terminate",
                                 "terminate"])
        [last_op] = [i for i, c in enumerate(self.fake.calls)
                     if c[:3] == ("op", "7", "add-reflector:verify.sh")]
        first_move = self.fake.calls.index(("terminate", "7", "node1",
                                            "127.0.0.1"))
        self.assertLess(last_op, first_move)       # reopened after the ops

    def test_each_world_keeps_its_guests_logs(self):
        for w, sim in (("w0", "7"), ("w1", "8")):
            for h in VMS:
                with self.subTest(world=w, vm=h):
                    self.assertEqual(
                        (self.run_dir / w / "logs" / f"{h}.log").read_text(),
                        f"journal of {h} in {sim}\n")

    def test_the_view_points_at_the_tunnel_and_the_lan(self):
        view = json.loads((self.run_dir / "tor-link" / "session.json")
                          .read_text())
        node2 = view["nodes"]["node2"]
        self.assertEqual(tuple(node2), NODE_FIELDS)
        self.assertRegex(node2["endpoint"], r"^tcp:127\.0\.0\.1:3\d+$")
        self.assertEqual(node2["lan_endpoint"], "tcp:10.77.0.11:1791")


class TestWorldLifetime(Lab):
    def test_keep_leaves_the_simulation_and_its_tunnels_up(self):
        fake = self.lab()
        code, _, _, _ = run(self.tmp, fake, ["tor-link"], keep=True)
        self.assertEqual(code, 0)
        self.assertFalse([c for c in fake.calls if c[:2] == ("netsim", "kill")])
        self.assertTrue(fake.procs)
        self.assertFalse(any(p.terminated for p in fake.procs))

    def test_a_simulation_that_will_not_die_stops_the_next_world(self):
        # why: the next world would boot beside it — three more guests, and
        # netsim commands that may land in either simulation
        fake = self.lab()
        immortal = fake.netsim

        def netsim(*args, **kw):
            if args[0] == "kill":
                fake.calls.append(("netsim", *map(str, args)))
                return ""                     # killed, and still listed
            return immortal(*args, **kw)
        fake.netsim = netsim
        code, doc, _, ran = run(self.tmp, fake, ["tor-link", "nat-punch"])
        self.assertNotEqual(code, 0)
        self.assertEqual(len([c for c in fake.calls
                              if c[:2] == ("netsim", "task")]), 1)
        self.assertEqual([(e["world"], e["status"]) for e in doc["entries"]],
                         [("w0", "pass")] * 3 + [("w1", "skipped")] * 3)
        self.assertIn("w0 failed to close", doc["aborted"])
        self.assertIn("teardown: ExecutorError: simulation 7 is still up",
                      doc["aborted"])

    def test_a_simulation_somebody_else_runs_refuses_the_boot(self):
        # why: netsim attaches commands to the newest simulation, so a world
        # booted beside another one could run its tests against it
        fake = self.lab(running=("3",))
        code, doc, _, ran = run(self.tmp, fake, ["tor-link"])
        self.assertEqual(code, 1)
        self.assertFalse([c for c in fake.calls if c[:2] == ("netsim", "task")])
        first, *rest = doc["entries"]
        self.assertEqual((first["status"], first["failure_kind"]),
                         ("fail", "environment"))
        self.assertTrue(all(e["status"] == "skipped" for e in rest))
        log = (self.tmp / "results").glob("*/bootstrap-user-software-key/"
                                          "driver.log")
        self.assertIn("netsim already runs simulation 3",
                      next(log).read_text())
        self.assertEqual(ran, [])

    def test_a_failed_boot_is_not_retried_by_each_later_step(self):
        # why: two root observers share a world but not a lineage, so the
        # second still prepares after the first one's boot failed
        fake = self.lab()
        with mock.patch.object(VmWorld, "_base_stage",
                               side_effect=ExecutorError("bake failed")):
            _, doc, _, _ = run(self.tmp, fake, ["smoke", "claim-window"],
                               vm=True)
        self.assertEqual([(e["test"], e["failure_kind"])
                          for e in doc["entries"]],
                         [("claim-window", "environment"),
                          ("smoke", "environment")])
        self.assertEqual(len([c for c in fake.calls
                              if c[:2] == ("netsim", "ps")]), 1)


class TestClose(Lab):
    def booted(self) -> tuple:
        fake = self.lab(stages=("null", LAB))
        stack = contextlib.ExitStack()
        self.addCleanup(stack.close)
        for p in fake.patches():
            stack.enter_context(p)
        world = VmWorld(self.tmp / "w0", self.tmp / "astrald", "abc1234")
        world.prepare(SimpleNamespace(name="bootstrap", nodes=["node1"],
                                      ops=[]))
        return fake, world

    def test_a_close_stopped_mid_log_pull_still_kills(self):
        fake, world = self.booted()
        with mock.patch.object(world, "_pull_logs",
                               side_effect=KeyboardInterrupt), \
                self.assertRaises(KeyboardInterrupt):
            world.teardown(False)
        self.assertIn(("netsim", "kill", world.sim), fake.calls)
        self.assertEqual(fake.sims, {})

    def test_a_simulation_already_reaped_closes_cleanly(self):
        fake, world = self.booted()
        fake.sims.clear()                    # gone before the close asked
        world.teardown(False)
        self.assertIn(("netsim", "kill", world.sim), fake.calls)


class TestLabStage(Lab):
    def test_a_missing_lab_is_baked_under_its_recipe_hash(self):
        fake = self.lab()
        world = VmWorld(self.tmp / "w0", self.tmp / "astrald", "abc1234")
        with mock.patch.object(vm, "_netsim", fake.netsim):
            self.assertEqual(world._base_stage(), LAB)
        [story] = [c for c in fake.calls if c[:2] == ("netsim", "story")]
        self.assertEqual(story[story.index("--save") + 1], LAB)

    def test_a_baked_lab_is_reused_without_a_rebake(self):
        fake = self.lab(stages=("null", LAB))
        world = VmWorld(self.tmp / "w0", self.tmp / "astrald", "abc1234")
        with mock.patch.object(vm, "_netsim", fake.netsim):
            self.assertEqual(world._base_stage(), LAB)
        self.assertFalse([c for c in fake.calls if c[:2] == ("netsim",
                                                            "story")])

    def test_a_stale_lab_name_is_not_booted(self):
        # why: a stage baked from another recipe is another world, so only
        # the current recipe's hash may stand in for the lab
        fake = self.lab(stages=("astrald-lab", "e2e-lab-r0"))
        world = VmWorld(self.tmp / "w0", self.tmp / "astrald", "abc1234")
        with mock.patch.object(vm, "_netsim", fake.netsim):
            self.assertEqual(world._base_stage(), LAB)


class TestPush(Lab):
    """One booted world, every guest-side effect recorded."""

    def booted(self) -> tuple:
        fake = self.lab(stages=("null", LAB))
        stack = contextlib.ExitStack()
        self.addCleanup(stack.close)
        for p in fake.patches():
            stack.enter_context(p)
        return fake, VmWorld(self.tmp / "w0", self.tmp / "astrald", "abc1234")

    def manifest(self, name, nodes):
        return SimpleNamespace(name=name, nodes=nodes, ops=[])

    def test_the_binaries_reach_every_vm_once_per_boot(self):
        fake, world = self.booted()
        world.prepare(self.manifest("bootstrap", ["node1"]))
        world.prepare(self.manifest("adopt", ["node1", "node2"]))
        self.assertEqual(len([c for c in fake.calls if c[0] == "scp"]),
                         2 * len(VMS))
        self.assertEqual(world.pushed, VMS)

    def test_a_roster_naming_no_running_vm_is_refused(self):
        _, world = self.booted()
        with self.assertRaises(ExecutorError):
            world.prepare(self.manifest("stray", ["node9"]))

    def test_root_sessions_carry_each_vms_own_token(self):
        # why: since 77953492 a token-less astral-query is held to
        # anonymous_allowlist, and the vmops run it over root's ssh
        fake, world = self.booted()
        world.prepare(self.manifest("bootstrap", ["node1"]))
        for h in VMS:
            [script] = [c[6] for c in fake.calls
                        if c[:2] == ("netsim", "ssh") and c[4] == h
                        and c[6].startswith("set -e")]
            with self.subTest(vm=h):
                self.assertIn(vm.SSHD_DROPIN, script)
                self.assertIn("Match User root\n    SetEnv "
                              f"ASTRALD_APPHOST_TOKEN={world._nodes[h]['token']}"
                              "\nMatch all\n", script)
                self.assertIn("/var/lib/astrald/config/apphost.yaml", script)
                for line in (f"chmod 600 {vm.SSHD_DROPIN}",
                             "chmod 600 /var/lib/astrald/config/apphost.yaml",
                             "sshd -t", "systemctl reload ssh",
                             "systemctl restart astrald"):
                    self.assertIn(f"\n{line}\n", script + "\n")

    def test_nodes_lists_only_the_tunneled_roster(self):
        # why: every VM holds a token, but a VM no roster names has no tunnel,
        # and a view entry for it would point a driver at port 0
        _, world = self.booted()
        world.prepare(self.manifest("adopt", ["node1", "node2"]))
        self.assertEqual(sorted(world.nodes()), ["node1", "node2"])
        self.assertIsNone(world.nodes()["node2"]["restarts"])


class TestNoStageCache(unittest.TestCase):
    def test_the_stage_cache_and_the_pinned_stage_are_gone(self):
        for f in sorted((TESTS / "lib").rglob("*.py")):
            text = f.read_text()
            for gone in ("stage_key", "NETSIM_E2E_SIM", '"stage:"',
                         "startswith(\"stage:\")"):
                with self.subTest(file=f.name, gone=gone):
                    self.assertNotIn(gone, text)


if __name__ == "__main__":
    unittest.main()
