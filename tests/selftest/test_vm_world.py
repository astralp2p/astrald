"""VM worlds, checked without a VM.

A fake lab stands in for netsim, ssh and the guests, and records every call
in order. The real runner drives the real VmWorld against it, so these
assertions pin the netsim call sequence a run makes — boot, push, tunnels,
ops, logs, kill, save — and what a run records, without booting anything.
"""
import argparse
import base64
import contextlib
import io
import json
import os
import re
import tempfile
import unittest
from pathlib import Path
from types import SimpleNamespace
from unittest import mock

from lib import runner
from lib.worlds import NODE_FIELDS, WorldError
from lib.worlds import vm
from lib.worlds.vm import VmWorld, recipe_hash

TESTS = Path(__file__).resolve().parent.parent
VMS = ["node1", "node2", "reflector"]
LAB = f"e2e-lab-r{recipe_hash(vm.RECIPE)}"


class FakeProc:
    """An `ssh -L` that stays up until terminated, which it records."""

    terminated = False
    dead = False            # its guest went down under it

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
        self.records = {}       # stage -> the world.json its guests hold
        self.written = {}       # sim -> the world.json last written into it
        self.procs = []
        self.natted = set()     # (sim, vm) that ran enter-nat
        self.suffix = ""        # what netsim appends to a name taken meanwhile
        # the identity each guest's apphost answers with, by tunnel port
        self.identity = lambda port: f"id-{port}"

    # -- netsim ------------------------------------------------------------

    def netsim(self, *args, check=True, capture=True, timeout=None) -> str:
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
        elif verb == "save":
            name = args[-1] + self.suffix
            self.stages.add(name)
            self.records[name] = self.written.get(args[2])
            # note: netsim relaunches the VMs, so every ssh -L dies
            for p in self.procs:
                p.dead = True
            return name
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
        if script.startswith(f"cat {vm.GUEST_RECORD}"):
            return self.records.get(self.sims[sim]) or ""
        if f"> {vm.GUEST_RECORD}.new" in script:
            payload = re.search(r"printf '%s' '([^']*)'", script).group(1)
            self.written[sim] = base64.b64decode(payload).decode()
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
    runner.main over the fake lab, its results under tmp. Saved worlds stay
    beside the lab, so a later run finds what an earlier one saved."""
    args = argparse.Namespace(**{
        "selection": selection, "driver": "script", "target": "fresh",
        "keep": False, "plan": False, "isolate": False, "prereqs": "script",
        "vm": False, "save": None, "from_stage": None, **opts})
    ran = []

    def step(py, script, log, env, timeout):
        ran.append((script.parent.name, script.stem))
        return 0

    with contextlib.ExitStack() as stack:
        for p in lab.patches():
            stack.enter_context(p)
        for p in (mock.patch.object(runner, "RESULTS", tmp / "results"),
                  mock.patch.object(runner, "SNAPSHOTS",
                                    lab.home.parent / "snapshots"),
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
        # booted only the first world; nothing but --save writes a stage now
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


class TestSnapshots(Lab):
    """`--save` a VM world, then run past its history with `--from`."""

    def save(self, fake: FakeLab) -> tuple:
        return run(self.tmp, fake, ["adopt-node"], vm=True, save="gw")

    def test_save_writes_the_record_root_only_before_netsim_saves(self):
        fake = self.lab()
        code, doc, _, _ = self.save(fake)
        self.assertEqual((code, doc["saved"]), (0, "gw"))
        writes = [i for i, c in enumerate(fake.calls)
                  if c[:2] == ("netsim", "ssh") and vm.GUEST_RECORD in c[6]
                  and "base64 -d" in c[6]]
        [save] = [i for i, c in enumerate(fake.calls)
                  if c[:2] == ("netsim", "save")]
        [kill] = [i for i, c in enumerate(fake.calls)
                  if c[:2] == ("netsim", "kill")]
        self.assertEqual(len(writes), len(VMS))
        self.assertLess(max(writes), save)
        self.assertLess(save, kill)
        script = fake.calls[writes[0]][6]
        self.assertIn("install -d -m 700 /var/lib/astral-tests", script)
        self.assertIn("umask 077", script)
        record = json.loads(fake.records["gw"])
        self.assertEqual([h["test"] for h in record["history"]],
                         ["bootstrap-user-software-key", "adopt-node"])
        host = json.loads((self.tmp / "snapshots" / "gw.json").read_text())
        self.assertEqual(host, record)

    def test_from_runs_only_what_the_saved_world_has_not(self):
        fake = self.lab()
        self.save(fake)
        fake.calls.clear()
        code, doc, run_dir, ran = run(self.tmp, fake, ["gateway-relay"],
                                      from_stage="gw")
        self.assertEqual(code, 0)
        boots = [c for c in fake.calls if c[:2] == ("netsim", "task")]
        self.assertEqual([c[c.index("--stage") + 1] for c in boots], ["gw"])
        self.assertEqual(ran, [("gateway-relay", "script"),
                               ("gateway-relay", "verify")])
        self.assertEqual([(e["test"], e["status"]) for e in doc["entries"]],
                         [("gateway-relay", "pass")])
        self.assertEqual((doc["from"], doc["hermetic"]), ("gw", False))
        world = json.loads((run_dir / "w0" / "world.json").read_text())
        self.assertEqual([h["test"] for h in world["history"]],
                         ["bootstrap-user-software-key", "adopt-node",
                          "gateway-relay"])
        # the saved world's facts reach the lineage of the first step run
        view = json.loads((run_dir / "gateway-relay" / "session.json")
                          .read_text())
        self.assertEqual(set(view["facts"]), set().union(
            *json.loads(fake.records["gw"])["facts"].values()))

    def test_from_refuses_a_selection_its_history_does_not_start(self):
        fake = self.lab()
        self.save(fake)
        for selection in (["smoke"], ["tor-link", "nat-punch"],
                          ["adopt-node"]):
            with self.subTest(selection=selection):
                code, _, _, _ = run(self.tmp / "x", fake, selection,
                                    from_stage="gw")
                self.assertEqual(code, 2)

    def test_a_guest_record_that_disagrees_refuses_the_boot(self):
        # why: the stage was saved again since this host's copy — same
        # history, other facts — and a resumed run takes its facts from it
        fake = self.lab()
        self.save(fake)
        record = json.loads(fake.records["gw"])
        record["facts"]["bootstrap-user-software-key"]["user_token"] = "other"
        fake.records["gw"] = json.dumps(record)
        code, doc, _, ran = run(self.tmp / "y", fake, ["gateway-relay"],
                                from_stage="gw")
        self.assertEqual(code, 1)
        [e] = doc["entries"]
        self.assertEqual((e["status"], e["failure_kind"], e["phase"]),
                         ("fail", "environment", "prepare"))
        self.assertEqual(ran, [])

    def test_a_kept_save_reopens_the_tunnels_netsim_killed(self):
        # why: netsim save relaunches the VMs, so a kept world's record would
        # point a debugger at dead ports
        fake = self.lab()
        code, doc, run_dir, _ = run(self.tmp, fake, ["adopt-node"], vm=True,
                                    save="gw", keep=True)
        self.assertEqual((code, doc["saved"]), (0, "gw"))
        [save] = [i for i, c in enumerate(fake.calls)
                  if c[:2] == ("netsim", "save")]
        after = [c for c in fake.calls[save:] if c[0] in ("tunnel",
                                                          "terminate")]
        self.assertEqual(after, [
            ("terminate", "7", "node1", "127.0.0.1"),
            ("terminate", "7", "node2", "127.0.0.1"),
            ("tunnel", "7", "node1", "127.0.0.1"),
            ("tunnel", "7", "node2", "127.0.0.1")])
        live = {p.tunnel[1]: p for p in fake.procs[-2:]}
        self.assertFalse(any(p.dead or p.terminated for p in live.values()))
        kept = json.loads((run_dir / "w0" / "world.json").read_text())
        host = json.loads((self.tmp / "snapshots" / "gw.json").read_text())
        self.assertNotEqual(kept["nodes"], host["nodes"])
        self.assertEqual(kept["nodes"]["node1"]["endpoint"],
                         f"tcp:127.0.0.1:{30000 + len(fake.procs) - 1}")

    def test_the_name_netsim_saved_under_is_the_one_recorded(self):
        fake = self.lab()
        fake.suffix = "-1"                   # taken between check and save
        code, doc, _, _ = self.save(fake)
        self.assertEqual((code, doc["saved"]), (0, "gw-1"))
        self.assertTrue((self.tmp / "snapshots" / "gw-1.json").is_file())

    def test_save_refuses_a_name_netsim_already_holds(self):
        code, doc, _, _ = run(self.tmp, self.lab(stages=("null", "gw")),
                              ["adopt-node"], vm=True, save="gw")
        self.assertEqual((code, doc), (2, None))

    def test_a_world_that_failed_is_not_saved(self):
        fake = self.lab()

        def silent(port):
            raise WorldError("apphost never answered")
        fake.identity = silent
        code, doc, _, _ = run(self.tmp, fake, ["adopt-node"], vm=True,
                              save="gw")
        self.assertNotIn("saved", doc)
        self.assertIn("save gw:", doc["aborted"])
        self.assertNotIn("gw", fake.stages)


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
        self.assertIn("teardown: WorldError: simulation 7 is still up",
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
        self.assertEqual(fake.sims, {"3": None})
        self.assertFalse([c for c in fake.calls if c[:2] == ("netsim", "kill")])

    def test_a_failed_boot_is_not_retried_by_each_later_step(self):
        # why: two root observers share a world but not a lineage, so the
        # second still prepares after the first one's boot failed
        fake = self.lab()
        with mock.patch.object(VmWorld, "_base_stage",
                               side_effect=WorldError("bake failed")) as bake:
            _, doc, _, _ = run(self.tmp, fake, ["smoke", "claim-window"],
                               vm=True)
        self.assertEqual([(e["test"], e["failure_kind"])
                          for e in doc["entries"]],
                         [("claim-window", "environment"),
                          ("smoke", "environment")])
        self.assertEqual(bake.call_count, 1)

    def test_a_world_after_a_failed_boot_still_boots(self):
        # why: the failed task left its simulation up under --keep, and the
        # next world would refuse to boot beside it
        fake = self.lab()
        stop(fake, "task", WorldError("netsim task: exit 1"), saved="-error")
        code, doc, _, _ = run(self.tmp, fake, ["tor-link", "nat-punch"])
        self.assertEqual(code, 1)
        self.assertEqual([(e["world"], e["status"]) for e in doc["entries"]],
                         [("w0", "fail")] + [("w0", "skipped")] * 2
                         + [("w1", "pass")] * 3)
        self.assertIn(("netsim", "kill", "7"), fake.calls)
        self.assertEqual(fake.sims, {})
        self.assertFalse({vm.SCRATCH_STAGE, f"{vm.SCRATCH_STAGE}-error"}
                         & fake.stages)


def stop(fake: FakeLab, verb: str, error: BaseException, saved: str = ""):
    """fake's first netsim `verb` runs, then raises error. A task's stage
    is then saved with `saved` appended, as netsim saves a failed task's."""
    real, armed = fake.netsim, [True]

    def netsim(*args, **kw):
        out = real(*args, **kw)
        if args[0] == verb and armed:
            armed.clear()
            if saved:
                fake.stages.discard(vm.SCRATCH_STAGE)
                fake.stages.add(vm.SCRATCH_STAGE + saved)
            raise error
        return out
    fake.netsim = netsim


class TestStoppedBoot(Lab):
    """A boot that stops after netsim created its simulation."""

    def ensure(self, fake: FakeLab) -> VmWorld:
        """A world's first ensure over fake, which has to raise."""
        stack = contextlib.ExitStack()
        self.addCleanup(stack.close)
        for p in fake.patches():
            stack.enter_context(p)
        stack.enter_context(contextlib.redirect_stderr(io.StringIO()))
        world = VmWorld(self.tmp / "w0", self.tmp / "astrald", "abc1234")
        with self.assertRaises(BaseException):
            world.ensure(SimpleNamespace(name="bootstrap", nodes=["node1"],
                                         ops=[]))
        return world

    def assert_clean(self, fake: FakeLab, world: VmWorld) -> None:
        self.assertIsNone(world.sim)
        self.assertIn(("netsim", "kill", "7"), fake.calls)
        self.assertEqual(fake.sims, {})
        self.assertEqual(fake.stages, {"null", LAB})

    def test_a_failed_task_kills_its_simulation_and_its_stage(self):
        fake = self.lab(stages=("null", LAB))
        stop(fake, "task", WorldError("netsim task: exit 1"), saved="-error")
        self.assert_clean(fake, self.ensure(fake))

    def test_a_ctrl_c_during_the_task_kills_its_simulation(self):
        fake = self.lab(stages=("null", LAB))
        stop(fake, "task", KeyboardInterrupt())
        self.assert_clean(fake, self.ensure(fake))

    def test_a_ctrl_c_during_the_scratch_removal_kills_the_simulation(self):
        fake = self.lab(stages=("null", LAB))
        stop(fake, "remove", KeyboardInterrupt())
        self.assert_clean(fake, self.ensure(fake))

    def test_a_second_new_simulation_is_killed_with_the_first(self):
        # why: neither was up before the boot, so both are this world's
        fake = self.lab(stages=("null", LAB))
        real = fake.netsim

        def netsim(*args, **kw):
            if args[0] == "task":
                fake.boot("null")
            return real(*args, **kw)
        fake.netsim = netsim
        world = self.ensure(fake)
        self.assertIsNone(world.sim)
        self.assertEqual(fake.sims, {})


class TestClose(Lab):
    def booted(self) -> tuple:
        fake = self.lab(stages=("null", LAB))
        stack = contextlib.ExitStack()
        self.addCleanup(stack.close)
        for p in fake.patches():
            stack.enter_context(p)
        world = VmWorld(self.tmp / "w0", self.tmp / "astrald", "abc1234")
        world.ensure(SimpleNamespace(name="bootstrap", nodes=["node1"],
                                      ops=[]))
        return fake, world

    def test_a_close_stopped_mid_log_pull_still_kills(self):
        fake, world = self.booted()
        with mock.patch.object(world, "_pull_logs",
                               side_effect=KeyboardInterrupt), \
                self.assertRaises(KeyboardInterrupt):
            world.close(False)
        self.assertIn(("netsim", "kill", world.sim), fake.calls)
        self.assertEqual(fake.sims, {})

    def test_a_simulation_already_reaped_closes_cleanly(self):
        fake, world = self.booted()
        fake.sims.clear()                    # gone before the close asked
        world.close(False)
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
        world.ensure(self.manifest("bootstrap", ["node1"]))
        world.ensure(self.manifest("adopt", ["node1", "node2"]))
        self.assertEqual(len([c for c in fake.calls if c[0] == "scp"]),
                         2 * len(VMS))
        self.assertEqual(world.pushed, VMS)

    def test_a_roster_naming_no_running_vm_is_refused(self):
        _, world = self.booted()
        with self.assertRaises(WorldError):
            world.ensure(self.manifest("stray", ["node9"]))

    def test_root_sessions_carry_each_vms_own_token(self):
        # why: since 77953492 a token-less astral-query is held to
        # anonymous_allowlist, and the vmops run it over root's ssh
        fake, world = self.booted()
        world.ensure(self.manifest("bootstrap", ["node1"]))
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
        world.ensure(self.manifest("adopt", ["node1", "node2"]))
        self.assertEqual(sorted(world.nodes()), ["node1", "node2"])
        self.assertIsNone(world.nodes()["node2"]["restarts"])


def pushed_files(fake: FakeLab, host: str) -> list:
    """{file: parsed config} of every push script sent to host, in order."""
    out = []
    for c in fake.calls:
        if c[:2] == ("netsim", "ssh") and c[4] == host \
                and c[6].startswith("set -e"):
            out.append({name: json.loads(text) for name, text in re.findall(
                r"cat > /var/lib/astrald/config/(\S+) <<'EOF'\n(.*?)EOF\n",
                c[6], re.S)})
    return out


class TestConfigPush(Lab):
    """A world's [config.*] overrides reach the guests they name."""

    def test_a_world_override_reaches_its_vm_with_one_restart(self):
        tests = runner.manifest.load_all(TESTS / "e2e")
        tests["tor-link"].config = {
            "node2": {"objects": {"label": "probe \U0001F680"}}}
        fake = self.lab()
        with mock.patch.object(runner.manifest, "load_all",
                               lambda _: tests):
            code, doc, _, _ = run(self.tmp, fake, ["tor-link"])
        self.assertEqual(code, 0)
        for h in VMS:
            with self.subTest(vm=h):
                push, = pushed_files(fake, h)       # one push per boot
                script, = [c[6] for c in fake.calls
                           if c[:2] == ("netsim", "ssh") and c[4] == h
                           and c[6].startswith("set -e")]
                self.assertEqual(script.count("systemctl restart astrald"), 1)
                want = {"apphost.yaml"} | ({"objects.yaml"} if h == "node2"
                                           else set())
                self.assertEqual(set(push), want)
        objects, = [p["objects.yaml"] for p in pushed_files(fake, "node2")]
        self.assertEqual(objects, {"label": "probe \U0001F680"})
        script, = [c[6] for c in fake.calls if c[:2] == ("netsim", "ssh")
                   and c[4] == "node2" and c[6].startswith("set -e")]
        # the file lands before the restart that reads it, and as the
        # character itself, since yaml.v2 refuses an escaped surrogate pair
        self.assertLess(script.index("config/objects.yaml"),
                        script.index("systemctl restart astrald"))
        # the operator must not read the world's config any more than its token
        self.assertIn("chmod 600 /var/lib/astrald/config/objects.yaml\n",
                      script)
        self.assertIn("\U0001F680", script)
        self.assertNotIn("\\ud83d", script)

    def test_an_apphost_override_keeps_the_harness_listener_and_token(self):
        fake = self.lab(stages=("null", LAB))
        with contextlib.ExitStack() as stack:
            for p in fake.patches():
                stack.enter_context(p)
            world = VmWorld(self.tmp / "w0", self.tmp / "astrald", "abc1234",
                            config={"node1": {"apphost": {
                                "allow_anonymous": False}}})
            world.ensure(SimpleNamespace(name="t", nodes=["node1"], ops=[]))
        apphost = pushed_files(fake, "node1")[0]["apphost.yaml"]
        self.assertEqual(apphost, {
            "listen": [f"tcp:127.0.0.1:{vm.APPHOST_PORT}"], "bind_http": "",
            "tokens": {world._nodes["node1"]["token"]: "localnode"},
            "allow_anonymous": False})
        self.assertNotIn("allow_anonymous",
                         pushed_files(fake, "node2")[0]["apphost.yaml"])


class TestConfigSnapshots(Lab):
    def test_a_saved_world_is_pushed_its_overrides_again_from_the_stage(self):
        tests = runner.manifest.load_all(TESTS / "e2e")
        tests["adopt-node"].config = {"node1": {"objects": {"x": 1}}}
        tests["gateway-relay"].config = {"node1": {"kcp": {"y": 2}}}
        fake = self.lab()
        with mock.patch.object(runner.manifest, "load_all", lambda _: tests):
            code, _, _, _ = run(self.tmp, fake, ["adopt-node"], vm=True,
                                save="gw")
            self.assertEqual(code, 0)
            saved = json.loads((self.tmp / "snapshots" / "gw.json")
                               .read_text())
            self.assertEqual(saved["config"],
                             {"node1": {"objects": {"x": 1}}})
            fake.calls.clear()
            code, _, _, _ = run(self.tmp, fake, ["gateway-relay"],
                                from_stage="gw")
        self.assertEqual(code, 0)
        boot = next(i for i, c in enumerate(fake.calls)
                    if c[:2] == ("netsim", "task"))
        self.assertIn("gw", fake.calls[boot])
        push, = pushed_files(fake, "node1")
        self.assertEqual((push["objects.yaml"], push["kcp.yaml"]),
                         ({"x": 1}, {"y": 2}))

    def test_a_history_configured_otherwise_since_the_save_is_refused(self):
        tests = runner.manifest.load_all(TESTS / "e2e")
        fake = self.lab()
        with mock.patch.object(runner.manifest, "load_all", lambda _: tests):
            self.assertEqual(run(self.tmp, fake, ["adopt-node"], vm=True,
                                 save="gw")[0], 0)
            tests["adopt-node"].config = {"node1": {"objects": {"x": 1}}}
            fake.calls.clear()
            code, _, _, _ = run(self.tmp, fake, ["gateway-relay"],
                                from_stage="gw")
        # refused while planning: nothing booted, and no run directory
        self.assertEqual(code, 2)
        self.assertFalse([c for c in fake.calls if c[:2] == ("netsim", "task")])
        self.assertEqual(len(list((self.tmp / "results").iterdir())), 1)


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
