"""The runner's per-test loop, driven against a fake world."""
import argparse
import contextlib
import io
import json
import tempfile
import unittest
from pathlib import Path
from types import SimpleNamespace
from unittest import mock

from lib import runner
from lib.worlds import WorldError
from lib.manifest import load_all

TESTS = Path(__file__).resolve().parent.parent


class FakeWorld:
    """A world that comes up, and a guest whose operator breaks before it
    drives."""

    pushed = ()
    torn_down = False

    def ensure(self, test) -> None:
        pass

    def nodes(self) -> dict:
        return {}

    def ssh(self, vm: str, script: str, check=True, timeout=None) -> str:
        raise WorldError("node1: operator model is not m after the rewrite")

    def boot_from(self, stage: str, record: dict) -> None:
        self.booted_from = stage

    def resume(self) -> dict:
        return {"history": [], "facts": {}}

    def dead(self) -> list:
        return []

    def close(self, keep: bool) -> None:
        self.torn_down = True


class TeardownFails(FakeWorld):
    def close(self, keep: bool) -> None:
        self.torn_down = True
        raise WorldError("netsim kill 1: exit 1")


class DiesUnderTest(FakeWorld):
    """A world whose daemon is dead by the time any oracle has spoken."""

    def dead(self) -> list:
        return ["node1"]


class PrepareFailsAndDies(DiesUnderTest):
    def ensure(self, test) -> None:
        raise WorldError("node1: never became ready")


class CrashesOnSecond(FakeWorld):
    """A world whose second prepare dies of something no step catches."""

    prepared = 0

    def ensure(self, test) -> None:
        self.prepared += 1
        if self.prepared == 2:
            raise RuntimeError("netsim ps: lost the simulation\nno space")


def run(selection: list, ex_class=FakeWorld, driver: str = "script",
        step=lambda *a, **kw: 0, target: str = "fresh") -> tuple:
    """(exit code, results doc, report text, world, world record) of one
    runner.main over a fake world, with every driver and oracle step
    answered by step."""
    args = argparse.Namespace(selection=selection, driver=driver,
                              target=target, keep=False)
    made = []

    def world(*_):
        made.append(ex_class())
        return made[-1]

    with tempfile.TemporaryDirectory() as tmp, \
            mock.patch.object(runner, "RESULTS", Path(tmp)), \
            mock.patch.object(runner, "vm_unavailable", return_value=None), \
            mock.patch.object(runner, "ensure_binary",
                              return_value=(Path(tmp) / "astrald", "abc")), \
            mock.patch.object(runner, "_attach_world", world), \
            mock.patch.object(runner, "_local_world", world), \
            mock.patch.object(runner, "_vm_world", world), \
            mock.patch.object(runner, "lease_ports",
                              return_value=SimpleNamespace(base=0)), \
            mock.patch.object(runner, "_run_step", step), \
            contextlib.redirect_stdout(io.StringIO()), \
            contextlib.redirect_stderr(io.StringIO()):
        try:
            code = runner.main(args)
        except KeyboardInterrupt:
            code = "interrupted"         # re-raised once the report is out
        except SystemExit:
            code = "exited"
        run_dir = next(Path(tmp).iterdir())
        doc = json.loads((run_dir / "results.json").read_text())
        text = (run_dir / "report.md").read_text()
        world = json.loads((run_dir / "w0" / "world.json").read_text())
    return code, doc, text, made[0], world


class TestOperatorError(unittest.TestCase):
    def test_an_operator_error_is_an_environment_failure_with_a_report(self):
        # why: the operator's ssh once raised outside the per-test try, so
        # the run left the loop with no verdict recorded and no report written
        args = argparse.Namespace(selection=["bootstrap-user-software-key"],
                                  driver="agent", target="fresh", keep=False)
        with tempfile.TemporaryDirectory() as tmp, \
                mock.patch.object(runner, "RESULTS", Path(tmp)), \
                mock.patch.object(runner, "vm_unavailable", return_value=None), \
            mock.patch.object(runner, "vm_unavailable", return_value=None), \
                mock.patch.object(runner, "ensure_binary",
                                  return_value=(Path(tmp) / "astrald", "abc")), \
                mock.patch.object(runner, "_vm_world",
                                  lambda *_: FakeWorld()), \
                contextlib.redirect_stdout(io.StringIO()):
            self.assertEqual(runner.main(args), 1)
            run_dir = next(Path(tmp).iterdir())
            doc = json.loads((run_dir / "results.json").read_text())
            self.assertTrue((run_dir / "report.md").is_file())
            log = (run_dir / "bootstrap-user-software-key" / "driver.log")
            self.assertIn("[runner] node1: operator model is not m",
                          log.read_text())
        [e] = doc["entries"]
        self.assertEqual((e["test"], e["status"], e["failure_kind"],
                          e["phase"], e["world"]),
                         ("bootstrap-user-software-key", "fail", "environment",
                          "driver", "w0"))


class TestPromptError(unittest.TestCase):
    def test_a_prompt_the_operator_cannot_be_handed_is_a_driver_failure(self):
        # why: the prompt is the test's defect, and it was recorded as the
        # world breaking
        with mock.patch.object(runner.agent, "render",
                               side_effect=runner.agent.PromptError("x")):
            _, doc, _, _, _ = run(["bootstrap-user-software-key"],
                                  driver="agent")
        [e] = doc["entries"]
        self.assertEqual((e["status"], e["failure_kind"], e["phase"]),
                         ("fail", "driver", "driver"))


class TestCrashSafe(unittest.TestCase):
    """A run that dies still writes its results and its report."""

    def test_a_crash_skips_the_rest_and_reports_incomplete(self):
        # why: an error no step catches — save() once raised outside the
        # loop's try — skipped finalize, and only events.jsonl survived
        code, doc, text, ex, world = run(["adopt-node"],
                                         ex_class=CrashesOnSecond)
        self.assertNotEqual(code, 0)
        self.assertTrue(ex.torn_down)
        # the record is written on the abort path, and a skip never joins it
        self.assertEqual([h["test"] for h in world["history"]],
                         ["bootstrap-user-software-key"])
        first, second = doc["entries"]
        self.assertEqual((first["test"], first["status"]),
                         ("bootstrap-user-software-key", "pass"))
        self.assertEqual((second["test"], second["status"], second["phase"],
                          second["reason"]),
                         ("adopt-node", "skipped", "prepare",
                          "RuntimeError: netsim ps: lost the simulation"))
        self.assertEqual(doc["aborted"], second["reason"])
        self.assertIn("— INCOMPLETE", text)
        self.assertIn("- `adopt-node` — RuntimeError: netsim ps", text)

    def test_ctrl_c_tears_down_and_reports_the_rest_as_interrupted(self):
        calls = []

        def step(py, script, log, env, timeout):
            calls.append(script)
            if len(calls) == 3:          # adopt-node's driver
                raise KeyboardInterrupt
            return 0

        code, doc, text, ex, _ = run(["adopt-node"], step=step)
        self.assertEqual(code, "interrupted")
        self.assertTrue(ex.torn_down)
        self.assertEqual([(e["test"], e["status"], e["phase"], e.get("reason"))
                          for e in doc["entries"]],
                         [("bootstrap-user-software-key", "pass", "verify",
                           None),
                          ("adopt-node", "skipped", "driver", "interrupted")])
        self.assertEqual(doc["aborted"], "interrupted")
        self.assertIn("— INCOMPLETE", text)
        self.assertIn("The run ended early: interrupted.", text)

    def test_a_raising_teardown_is_recorded_and_never_reads_as_pass(self):
        code, doc, text, ex, _ = run(["smoke"], ex_class=TeardownFails)
        self.assertNotEqual(code, 0)
        self.assertEqual(doc["entries"][0]["status"], "pass")
        self.assertEqual(doc["aborted"],
                         "teardown: WorldError: netsim kill 1: exit 1")
        self.assertIn("— INCOMPLETE", text)

    def test_pushed_survives_an_abort(self):
        class PushedCrashes(PushingWorld, CrashesOnSecond):
            pass

        _, doc, _, _, _ = run(["adopt-node"], ex_class=PushedCrashes)
        self.assertIn("aborted", doc)
        self.assertEqual(doc["pushed"], "node1,node2,reflector")

    def test_a_world_dead_under_a_test_fails_it_at_alive(self):
        _, doc, text, _, _ = run(["smoke"], ex_class=DiesUnderTest)
        [e] = doc["entries"]
        self.assertEqual((e["status"], e["failure_kind"], e["phase"]),
                         ("fail", "environment", "alive"))
        self.assertIn("### smoke — environment in alive", text)

    def test_an_earlier_environment_failure_keeps_its_phase(self):
        _, doc, _, _, _ = run(["smoke"], ex_class=PrepareFailsAndDies)
        self.assertEqual(doc["entries"][0]["phase"], "prepare")

    def test_a_stop_the_loop_does_not_catch_still_skips_the_rest(self):
        # why: a SystemExit left the except untouched, and the finally wrote
        # a PASS report over a run that had recorded nothing
        def step(*a, **kw):
            raise SystemExit(3)

        code, doc, text, _, _ = run(["smoke"], step=step)
        self.assertEqual(code, "exited")
        self.assertEqual((doc["entries"][0]["status"],
                          doc["entries"][0]["phase"]), ("skipped", "driver"))
        self.assertIn("— INCOMPLETE", text)

    def test_every_record_carries_its_world_and_phase(self):
        def step(py, script, log, env, timeout):
            return 1 if script.parent.name == "smoke" and \
                script.name == "verify.py" else 0

        code, doc, _, _, _ = run(["smoke", "adopt-node"], step=step)
        self.assertEqual(code, 1)
        self.assertNotIn("aborted", doc)
        self.assertEqual({e["world"] for e in doc["entries"]}, {"w0"})
        self.assertEqual({e["test"]: (e["status"], e["phase"])
                          for e in doc["entries"]},
                         {"smoke": ("fail", "verify"),
                          "bootstrap-user-software-key": ("pass", "verify"),
                          "adopt-node": ("pass", "verify")})

    def test_a_lineage_skip_names_the_failed_test(self):
        def step(py, script, log, env, timeout):
            return 1 if script.parent.name == "bootstrap-user-software-key" \
                else 0

        _, doc, text, _, _ = run(["adopt-node"], step=step)
        skipped = doc["entries"][1]
        self.assertEqual((skipped["test"], skipped["status"], skipped["phase"],
                          skipped["reason"]),
                         ("adopt-node", "skipped", "prepare",
                          "bootstrap-user-software-key failed in w0"))
        self.assertIn("- `adopt-node` — bootstrap-user-software-key failed in "
                      "w0", text)


class TestDriverFacts(unittest.TestCase):
    def test_a_driver_that_leaves_broken_facts_fails_alone(self):
        # why: the facts were read outside the per-test handling, so one
        # driver's file stopped the run and skipped every later world
        for broken in (b"", b'{"user_id": "x"', b'["user_id"]', b"\xff"):
            with self.subTest(facts=broken):
                def step(py, script, log, env, timeout):
                    if script.parent.name == "bootstrap-user-software-key" \
                            and script.name == "script.py":
                        Path(env["ASTRAL_TESTS_FACTS_OUT"]).write_bytes(broken)
                    return 0

                code, doc, _, _, _ = run(["adopt-node", "smoke"], step=step)
                self.assertEqual(code, 1)
                self.assertNotIn("aborted", doc)
                self.assertEqual(
                    [(e["test"], e["status"], e.get("failure_kind"),
                      e["phase"]) for e in doc["entries"]],
                    [("smoke", "pass", None, "verify"),
                     ("bootstrap-user-software-key", "fail", "driver",
                      "driver"),
                     ("adopt-node", "skipped", None, "prepare")])


class TestPortsBusy(unittest.TestCase):
    def test_a_run_refused_for_ports_leaves_no_run_directory(self):
        # why: a refused run left an empty results/<stamp>/, with no report
        # to say why, as the newest run
        args = argparse.Namespace(selection=["smoke"], driver="script",
                                  target="fresh", keep=False)
        busy = runner.PortsBusy("no free port span")
        with tempfile.TemporaryDirectory() as tmp, \
                mock.patch.object(runner, "RESULTS", Path(tmp) / "results"), \
                mock.patch.object(runner, "ensure_binary",
                                  return_value=(Path(tmp) / "astrald", "abc")), \
                mock.patch.object(runner, "lease_ports", side_effect=busy), \
                contextlib.redirect_stderr(io.StringIO()):
            self.assertEqual(runner.main(args), 2)
            self.assertFalse((Path(tmp) / "results").exists())


class TestAttachWorld(unittest.TestCase):
    def test_attach_runs_its_one_world_through_the_loop(self):
        _, doc, _, _, world = run(["smoke"], target="attach")
        [e] = doc["entries"]
        self.assertEqual((e["test"], e["env"], e["world"], e["status"]),
                         ("smoke", "node", "w0", "pass"))
        self.assertEqual([h["test"] for h in world["history"]], ["smoke"])

    def test_a_selection_attach_would_have_to_build_is_refused(self):
        # why: attach checks the state a daemon stands at and never builds
        # one, so a prereq is a plan error before anything is touched
        args = argparse.Namespace(selection=["adopt-node"], driver="script",
                                  target="attach", keep=False)
        err = io.StringIO()
        with mock.patch.object(runner, "_attach_world",
                               lambda: self.fail("nothing may open")), \
                contextlib.redirect_stderr(err):
            self.assertEqual(runner.main(args), 2)
        self.assertIn("would have to build 'bootstrap-user-software-key'",
                      err.getvalue())


class TestAgentRuns(unittest.TestCase):
    def test_prereqs_run_by_script_and_the_selected_test_by_the_operator(self):
        # why: rule 7 — an agent run follows the plan, so its prereqs build
        # the world by script unless --prereqs agent asks otherwise
        _, doc, _, _, _ = run(["adopt-node"], driver="agent")
        self.assertEqual([(e["test"], e["kind"], e["env"], e["driver"],
                           e["status"], e.get("failure_kind"))
                          for e in doc["entries"]],
                         [("bootstrap-user-software-key", "prereq", "netsim",
                           "script", "pass", None),
                          ("adopt-node", "test", "netsim", "agent", "fail",
                           "environment")])


class PushingWorld(FakeWorld):
    """A VM world: the binaries under test went to three machines."""

    pushed = ["node1", "node2", "reflector"]


class TestPushedHeader(unittest.TestCase):
    def test_the_header_names_every_vm_the_binaries_reached(self):
        args = argparse.Namespace(selection=["bootstrap-user-software-key"],
                                  driver="agent", target="fresh", keep=False)
        with tempfile.TemporaryDirectory() as tmp, \
                mock.patch.object(runner, "RESULTS", Path(tmp)), \
                mock.patch.object(runner, "vm_unavailable", return_value=None), \
            mock.patch.object(runner, "vm_unavailable", return_value=None), \
                mock.patch.object(runner, "ensure_binary",
                                  return_value=(Path(tmp) / "astrald", "abc")), \
                mock.patch.object(runner, "_vm_world",
                                  lambda *_: PushingWorld()), \
                contextlib.redirect_stdout(io.StringIO()):
            runner.main(args)
            run_dir = next(Path(tmp).iterdir())
            doc = json.loads((run_dir / "results.json").read_text())
        self.assertEqual(doc["pushed"], "node1,node2,reflector")


class TestDriverEnv(unittest.TestCase):
    AMBIENT = {"ASTRAL_ENDPOINT": "tcp:127.0.0.1:8625",
               "ASTRALD_APPHOST_ENDPOINT": "tcp:127.0.0.1:8625",
               "ASTRALD_APPHOST_TOKEN": "developer-token"}

    def test_a_run_from_a_saved_world_hands_its_drivers_a_stripped_env(self):
        # why through main(): the defect was the call site, which passed the
        # header's hermetic — false for a world an earlier run built, as it
        # was under the retired stage: target — and stripped nothing
        args = argparse.Namespace(selection=["smoke"], driver="script",
                                  target="fresh", keep=False, from_stage="gw")
        seen = []

        def run_step(py, script, log, env, timeout):
            seen.append(env)
            return 0

        with tempfile.TemporaryDirectory() as tmp, \
                mock.patch.dict("os.environ", self.AMBIENT), \
                mock.patch.object(runner, "RESULTS", Path(tmp) / "results"), \
                mock.patch.object(runner, "SNAPSHOTS", Path(tmp)), \
                mock.patch.object(runner, "vm_unavailable", return_value=None), \
                mock.patch.object(runner, "stages", return_value={"gw"}), \
                mock.patch.object(runner, "ensure_binary",
                                  return_value=(Path(tmp) / "astrald", "abc")), \
                mock.patch.object(runner, "_vm_world",
                                  lambda *_: FakeWorld()), \
                mock.patch.object(runner, "_run_step", run_step), \
                contextlib.redirect_stdout(io.StringIO()):
            (Path(tmp) / "gw.json").write_text(
                json.dumps({"history": [], "facts": {}}))
            self.assertEqual(runner.main(args), 0)
            [run_dir] = (Path(tmp) / "results").iterdir()
            doc = json.loads((run_dir / "results.json").read_text())
        self.assertEqual((doc["hermetic"], doc["from"]), (False, "gw"))
        self.assertEqual(len(seen), 2)              # driver, then oracle
        for env in seen:
            for var in self.AMBIENT:
                self.assertNotIn(var, env)

    def env_for(self, target: str) -> dict:
        with mock.patch.dict("os.environ", self.AMBIENT):
            return runner.driver_env(Path("/s/session.json"), target)

    def test_ambient_endpoint_and_token_are_stripped_under_every_target(self):
        # why: a developer's token answered for a test's anonymous connect
        # under the retired stage: target, which stripped nothing because it
        # is not hermetic, and under attach, where smoke's anonymous
        # shell.spec then read as answered
        for target in ("fresh", "attach"):
            with self.subTest(target=target):
                env = self.env_for(target)
                for var in self.AMBIENT:
                    self.assertNotIn(var, env)
                self.assertEqual(env["ASTRAL_TESTS_SESSION"],
                                 "/s/session.json")


class TestBareSelections(unittest.TestCase):
    """Bare selections over the shipped manifests, as the planner orders
    them: the orderings the old walk once got wrong."""

    def setUp(self):
        self.all = load_all(TESTS / "e2e")

    def worlds(self, *selection) -> list:
        return [[s.test for s in w.steps]
                for w in runner.planner.plan(self.all, list(selection))]

    def test_a_destroyer_runs_after_the_test_it_would_break(self):
        # why: ordered by depth alone, expel-node ran first and mcp-peer met
        # a swarm with node2 already expelled
        [order] = self.worlds("expel-node", "mcp-peer")
        self.assertLess(order.index("mcp-peer"), order.index("expel-node"))
        self.assertEqual(order[-1], "expel-node")

    def test_an_observer_runs_before_a_producer_at_its_depth(self):
        # why: claim-window needs node1 unclaimed, and bootstrap claims it
        self.assertEqual(self.worlds("adopt-node", "claim-window"),
                         [["claim-window", "bootstrap-user-software-key",
                           "adopt-node"]])

    def test_two_destroyers_each_get_a_world(self):
        # why: the old runner refused this selection outright; each
        # destroyer now ends a world of its own
        self.assertEqual([w[-1] for w in self.worlds("expel-node",
                                                     "hold-purge")],
                         ["expel-node", "hold-purge"])


if __name__ == "__main__":
    unittest.main()
