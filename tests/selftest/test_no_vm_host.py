"""A host that runs no VM world: the probe, the whole-run refusal, and the
note --plan adds.

why a seam: the selftests put a stub netsim first on PATH (selftest
__init__), so the probe's PATH half never fires on its own; these tests
patch runner.vm_unavailable, or the probe's platform and which.
"""
import argparse
import contextlib
import io
import tempfile
import unittest
from pathlib import Path
from types import SimpleNamespace
from unittest import mock

from lib import runner
from lib.worlds import vm

TESTS = Path(__file__).resolve().parent.parent
WHY = "netsim runs only on Linux, and this host is Darwin"


class TestProbe(unittest.TestCase):
    def probe(self, system, netsim):
        with mock.patch.object(vm.platform, "system", return_value=system), \
                mock.patch.object(vm.shutil, "which", return_value=netsim):
            return vm.unavailable()

    def test_a_host_other_than_linux_is_named(self):
        self.assertEqual(self.probe("Darwin", "/usr/local/bin/netsim"), WHY)

    def test_linux_without_netsim_says_so(self):
        self.assertEqual(self.probe("Linux", None), "netsim is not on PATH")

    def test_linux_with_netsim_runs_vm_worlds(self):
        self.assertIsNone(self.probe("Linux", "/usr/bin/netsim"))


def refused(selection, **opts) -> tuple:
    """(exit code, stderr) of runner.main on a host that runs no VM world,
    with everything a run would touch made to fail the test."""
    args = argparse.Namespace(selection=selection, driver="script",
                              target="fresh", keep=False)
    for k, v in opts.items():
        setattr(args, k, v)
    err = io.StringIO()

    def touched(name):
        def fail(*_, **__):
            raise AssertionError(f"a refused run reached {name}")
        return fail

    with tempfile.TemporaryDirectory() as tmp, \
            mock.patch.object(runner, "RESULTS", Path(tmp) / "results"), \
            mock.patch.object(runner, "vm_unavailable", return_value=WHY), \
            mock.patch.object(runner, "ensure_binary", touched("ensure_binary")), \
            mock.patch.object(runner, "lease_ports", touched("lease_ports")), \
            mock.patch.object(runner, "_vm_world", touched("_vm_world")), \
            mock.patch.object(runner, "fresh_run_dir", touched("fresh_run_dir")), \
            mock.patch.object(runner, "stages", touched("stages")), \
            contextlib.redirect_stderr(err), \
            contextlib.redirect_stdout(io.StringIO()):
        code = runner.main(args)
        left = (Path(tmp) / "results").exists()
    assert not left, "a refused run left a results directory"
    return code, err.getvalue()


class TestNoVmHost(unittest.TestCase):
    def test_a_test_with_ops_refuses_the_run_naming_it_and_its_ops(self):
        code, err = refused(["tor-link"])
        self.assertEqual(code, 2)
        self.assertIn(f"this host runs no VM world ({WHY})", err)
        self.assertIn("w0: tor-link runs VM ops (enable-tor node1 node2; "
                      "leave-lan node2 --peer node1)", err)
        self.assertIn("tests/README.md, Without netsim", err)

    def test_the_whole_run_is_refused_not_part_of_it(self):
        # why: smoke's local world would run and pass, and the run would
        # read as a red test rather than a host that cannot run it
        code, err = refused(["smoke", "tor-link"])
        self.assertEqual(code, 2)
        self.assertIn("tor-link runs VM ops", err)
        self.assertNotIn("smoke runs", err)

    def test_vm_names_the_flag(self):
        code, err = refused(["smoke"], vm=True)
        self.assertEqual(code, 2)
        self.assertIn("--vm puts every world in VMs", err)

    def test_save_and_from_name_their_flag_and_never_say_add_vm(self):
        for opts, flag in (({"save": "gw"}, "--save"),
                           ({"from_stage": "gw"}, "--from")):
            with self.subTest(flag=flag):
                code, err = refused(["adopt-node"], **opts)
                self.assertEqual(code, 2)
                self.assertIn(f"{flag} works on netsim stages", err)
                self.assertNotIn("add --vm", err)

    def test_the_agent_driver_names_the_lab_operator(self):
        code, err = refused(["agent.suite"], driver="agent")
        self.assertEqual(code, 2)
        self.assertIn("--driver agent runs the lab's operator, inside VM "
                      "node1", err)

    def test_a_local_selection_is_not_refused(self):
        made = []

        class World:
            pushed = ()

            def ensure(self, test): pass
            def nodes(self): return {}
            def dead(self): return []
            def close(self, keep): pass

        def world(*_):
            made.append(World())
            return made[-1]

        args = argparse.Namespace(selection=["smoke"], driver="script",
                                  target="fresh", keep=False)
        with tempfile.TemporaryDirectory() as tmp, \
                mock.patch.object(runner, "RESULTS", Path(tmp)), \
                mock.patch.object(runner, "vm_unavailable", return_value=WHY), \
                mock.patch.object(runner, "ensure_binary",
                                  return_value=(Path(tmp) / "astrald", "abc")), \
                mock.patch.object(runner, "_local_world", world), \
                mock.patch.object(runner, "lease_ports",
                                  return_value=SimpleNamespace(base=0)), \
                mock.patch.object(runner, "_run_step", lambda *a, **kw: 0), \
                contextlib.redirect_stdout(io.StringIO()), \
                contextlib.redirect_stderr(io.StringIO()):
            self.assertEqual(runner.main(args), 0)
        self.assertEqual(len(made), 1)


class TestPlanNote(unittest.TestCase):
    def plan(self, selection):
        args = argparse.Namespace(selection=selection, driver="script",
                                  target="fresh", keep=False, plan=True)
        out, err = io.StringIO(), io.StringIO()
        with mock.patch.object(runner, "vm_unavailable", return_value=WHY), \
                contextlib.redirect_stdout(out), \
                contextlib.redirect_stderr(err):
            code = runner.main(args)
        return code, out.getvalue(), err.getvalue()

    def test_plan_keeps_its_stdout_and_exit_and_notes_the_refusal(self):
        code, out, err = self.plan(["tor.suite"])
        self.assertEqual(code, 0)
        golden = (TESTS / "selftest" / "golden" / "tor.plan").read_text()
        self.assertEqual(out, golden)
        self.assertEqual(err, f"run: note: this host runs no VM world "
                              f"({WHY}); a run of this plan is refused at "
                              "w0\n")

    def test_a_local_plan_writes_no_note(self):
        code, _, err = self.plan(["main.suite"])
        self.assertEqual((code, err), (0, ""))


if __name__ == "__main__":
    unittest.main()
