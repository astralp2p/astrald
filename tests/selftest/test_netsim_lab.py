"""The netsim executor's lab: which stage it boots, and which VMs run the
binaries under test. Both are decided from netsim's own listings, so stubbing
netsim checks them without a VM."""
import json
import os
import tempfile
import unittest
from pathlib import Path
from types import SimpleNamespace
from unittest import mock

from lib.executors import netsim
from lib.executors.netsim import NetsimExecutor, recipe_hash

VMS = ["node1", "node2", "reflector"]


def executor(tmp: Path) -> NetsimExecutor:
    return NetsimExecutor(tmp / "session", tmp / "astrald", "abc1234")


def booted(tmp: Path) -> NetsimExecutor:
    """An executor over a booted simulation whose manifest lists VMS running
    and `spare` stopped, with every guest-side effect stubbed and recorded in
    `ex.calls`. Opening a session tunnels the roster, as the real one does."""
    ex = executor(tmp)
    ex.sim = "7"
    sim_dir = tmp / "netsim" / "sims" / "7"
    sim_dir.mkdir(parents=True)
    vms = [{"hostname": vm, "ip": f"10.77.0.{i + 10}", "state": "running"}
           for i, vm in enumerate(VMS)]
    vms.append({"hostname": "spare", "ip": "10.77.0.99", "state": "stopped"})
    (sim_dir / "manifest.json").write_text(json.dumps({"vms": vms}))
    ex.calls = []
    ex._materialize = lambda state, nodes: None
    ex._scp = lambda vm, src, dst: ex.calls.append(("scp", vm, dst))
    ex._ssh = (lambda vm, script, check=True:
               ex.calls.append(("ssh", vm, script)) or "")

    def open_session(nodes):
        for vm in nodes:
            ex._nodes[vm].update(port=20000 + len(vm), identity=f"{vm}-id")
    ex._open_session = open_session
    return ex


def test(name: str, nodes: list):
    return SimpleNamespace(name=name, env="node", start="null", nodes=nodes,
                           steps=[])


class TestLabStage(unittest.TestCase):
    def setUp(self):
        tmp = tempfile.TemporaryDirectory()
        self.addCleanup(tmp.cleanup)
        self.tmp = Path(tmp.name)
        self.calls = []

    def fake_netsim(self, stages):
        def run(*args, check=True, capture=True):
            self.calls.append(args)
            if args[:2] == ("stages", "--json"):
                return json.dumps([{"slug": s} for s in stages])
            return ""
        return run

    def test_a_missing_lab_is_baked_under_its_recipe_hash(self):
        ex = executor(self.tmp)
        want = f"e2e-lab-r{recipe_hash(ex.recipe)}"
        with mock.patch.object(netsim, "_netsim", self.fake_netsim([])):
            self.assertEqual(ex._base_stage(), want)
        story = [a for a in self.calls if a[0] == "story"]
        self.assertEqual(len(story), 1)
        self.assertEqual(story[0][story[0].index("--save") + 1], want)

    def test_a_baked_lab_is_reused_without_a_rebake(self):
        ex = executor(self.tmp)
        want = f"e2e-lab-r{recipe_hash(ex.recipe)}"
        with mock.patch.object(netsim, "_netsim",
                               self.fake_netsim(["null", want])):
            self.assertEqual(ex._base_stage(), want)
        self.assertFalse([a for a in self.calls if a[0] == "story"])

    def test_a_stale_lab_name_is_not_booted(self):
        # why: a stage baked from another recipe is another world, so only
        # the current recipe's hash may stand in for the lab
        ex = executor(self.tmp)
        with mock.patch.object(netsim, "_netsim",
                               self.fake_netsim(["astrald-lab", "e2e-lab-r0"])):
            self.assertEqual(ex._base_stage(),
                             f"e2e-lab-r{recipe_hash(ex.recipe)}")
        self.assertTrue([a for a in self.calls if a[0] == "story"])


class TestPush(unittest.TestCase):
    def setUp(self):
        tmp = tempfile.TemporaryDirectory()
        self.addCleanup(tmp.cleanup)
        self.tmp = Path(tmp.name)
        env = mock.patch.dict(os.environ, {"NETSIM_HOME": str(self.tmp / "netsim")})
        env.start()
        self.addCleanup(env.stop)

    def test_every_running_vm_in_the_manifest_is_a_push_target(self):
        self.assertEqual(booted(self.tmp)._vms(), VMS)

    def test_the_binaries_reach_every_vm_once_per_boot(self):
        ex = booted(self.tmp)
        ex.prepare(test("bootstrap", ["node1"]))
        ex.prepare(test("adopt", ["node1", "node2"]))
        scps = [(vm, dst) for kind, vm, *dst in ex.calls if kind == "scp"]
        self.assertEqual(sorted(scps), sorted(
            (vm, [dst]) for vm in VMS
            for dst in ("/usr/local/bin/astrald", "/usr/local/bin/astral-query")))
        self.assertEqual(ex.pushed, VMS)

    def test_a_roster_naming_no_running_vm_is_refused(self):
        ex = booted(self.tmp)
        for vm in ("node9", "spare"):
            with self.subTest(vm=vm), self.assertRaises(netsim.ExecutorError):
                ex.prepare(test("stray", [vm]))
        self.assertNotIn("spare", ex.pushed)

    def test_root_sessions_carry_each_vms_own_token(self):
        # why: since 77953492 a token-less astral-query is held to
        # anonymous_allowlist, and the vmops run it over root's ssh
        ex = booted(self.tmp)
        ex.prepare(test("bootstrap", ["node1"]))
        for vm in VMS:
            [script] = [c[2] for c in ex.calls if c[:2] == ("ssh", vm)]
            with self.subTest(vm=vm):
                self.assertIn(netsim.SSHD_DROPIN, script)
                self.assertIn("Match User root\n    SetEnv "
                              f"ASTRALD_APPHOST_TOKEN={ex._nodes[vm]['token']}"
                              "\nMatch all\n", script)
                self.assertIn("systemctl reload ssh", script)

    def test_the_session_lists_only_the_tunneled_roster(self):
        # why: every VM holds a token, but a VM no roster names has no tunnel,
        # and a session entry for it would point a driver at port 0
        ex = booted(self.tmp)
        ex.prepare(test("adopt", ["node1", "node2"]))
        facts = self.tmp / "facts.json"
        facts.write_text(json.dumps({"user_token": "t"}))
        ex.merge_facts(facts)
        doc = json.loads(ex.session_json_path.read_text())
        self.assertEqual(sorted(doc["nodes"]), ["node1", "node2"])
        self.assertEqual(doc["facts"], {"user_token": "t"})


if __name__ == "__main__":
    unittest.main()
