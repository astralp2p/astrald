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
from lib.executors.netsim import NetsimExecutor

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
    ex._ssh = lambda vm, script, check=True: ex.calls.append(("ssh", vm)) or ""

    def open_session(nodes):
        for vm in nodes:
            ex._nodes[vm].update(port=20000 + len(vm), identity=f"{vm}-id")
    ex._open_session = open_session
    return ex


def test(name: str, nodes: list):
    return SimpleNamespace(name=name, env="node", start="null", nodes=nodes,
                           steps=[])


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
