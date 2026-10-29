"""The runner's per-test loop, driven against a fake executor."""
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
from lib.executors import ExecutorError
from lib.manifest import load_all

TESTS = Path(__file__).resolve().parent.parent


class FakeExecutor:
    """A world that comes up, and an operator that breaks before it drives."""

    pushed = ()
    torn_down = False

    def use_operator(self, profile: dict) -> None:
        pass

    def want_model(self, model: str, base_url: str = "") -> None:
        pass

    def prepare(self, test) -> None:
        pass

    def nodes(self) -> dict:
        return {}

    def run_agent(self, test, log: Path, facts: Path, timeout: int) -> int:
        raise ExecutorError("node1: operator model is not m after the rewrite")

    def save(self, state: str) -> None:
        pass

    def broken(self) -> list:
        return []

    def teardown(self, keep: bool) -> None:
        self.torn_down = True


class TeardownFails(FakeExecutor):
    def teardown(self, keep: bool) -> None:
        self.torn_down = True
        raise ExecutorError("netsim kill 1: exit 1")


class DiesUnderTest(FakeExecutor):
    """A world whose daemon is dead by the time any oracle has spoken."""

    def broken(self) -> list:
        return ["node1"]


class PrepareFailsAndDies(DiesUnderTest):
    def prepare(self, test) -> None:
        raise ExecutorError("node1: never became ready")


class SaveFails(FakeExecutor):
    """A world whose snapshot breaks after the first producer passes."""

    def save(self, state: str) -> None:
        raise ExecutorError("netsim save --sim 1 e2e-one-node: exit 1\nno space")


def run(selection: list, ex_class=FakeExecutor, driver: str = "script",
        step=lambda *a, **kw: 0) -> tuple:
    """(exit code, results doc, report text, executor, world record) of one
    runner.main over a fake world, with every driver and oracle step
    answered by step."""
    args = argparse.Namespace(selection=selection, driver=driver,
                              target="fresh", keep=False)
    made = []

    def executor(*_):
        made.append(ex_class())
        return made[-1]

    with tempfile.TemporaryDirectory() as tmp, \
            mock.patch.object(runner, "RESULTS", Path(tmp)), \
            mock.patch.object(runner, "ensure_binary",
                              return_value=(Path(tmp) / "astrald", "abc")), \
            mock.patch.object(runner, "_executor", executor), \
            mock.patch.object(runner, "_local_world", executor), \
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
        # why: run_agent raised outside the per-test try, so the run left the
        # loop with no verdict recorded and no report written
        args = argparse.Namespace(selection=["bootstrap-user-software-key"],
                                  driver="agent", target="fresh", keep=False)
        with tempfile.TemporaryDirectory() as tmp, \
                mock.patch.object(runner, "RESULTS", Path(tmp)), \
                mock.patch.object(runner, "ensure_binary",
                                  return_value=(Path(tmp) / "astrald", "abc")), \
                mock.patch.object(runner, "_executor",
                                  lambda *_: FakeExecutor()), \
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


class TestCrashSafe(unittest.TestCase):
    """A run that dies still writes its results and its report."""

    def test_a_save_error_skips_the_rest_and_reports_incomplete(self):
        # why: save() raised outside the loop's try, after the loop's finally
        # had run, so finalize was skipped and only events.jsonl survived
        code, doc, text, ex, world = run(["adopt-node"], ex_class=SaveFails)
        self.assertNotEqual(code, 0)
        self.assertTrue(ex.torn_down)
        # the record is written on the abort path, and a skip never joins it
        self.assertEqual([h["test"] for h in world["history"]],
                         ["bootstrap-user-software-key"])
        first, second = doc["entries"]
        self.assertEqual((first["test"], first["status"]),
                         ("bootstrap-user-software-key", "pass"))
        self.assertEqual((second["test"], second["status"], second["reason"]),
                         ("adopt-node", "skipped",
                          "ExecutorError: netsim save --sim 1 e2e-one-node: "
                          "exit 1"))
        self.assertEqual(doc["aborted"], second["reason"])
        self.assertIn("— INCOMPLETE", text)
        self.assertIn("- `adopt-node` — ExecutorError: netsim save", text)

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
                         "teardown: ExecutorError: netsim kill 1: exit 1")
        self.assertIn("— INCOMPLETE", text)

    def test_pushed_survives_an_abort(self):
        class PushedSaveFails(PushingExecutor, SaveFails):
            pass

        _, doc, _, _, _ = run(["adopt-node"], ex_class=PushedSaveFails)
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


class TestExecutorPath(unittest.TestCase):
    def test_a_skip_on_the_executor_path_names_the_failed_test(self):
        # why: the executor path runs through the world loop and takes its
        # skip rule; it read "state one-node was never built" before
        _, doc, _, _, _ = run(["adopt-node"], driver="agent")
        failed, skipped = doc["entries"]
        self.assertEqual((failed["test"], failed["failure_kind"]),
                         ("bootstrap-user-software-key", "environment"))
        self.assertEqual((skipped["test"], skipped["env"], skipped["world"],
                          skipped["reason"]),
                         ("adopt-node", "netsim", "w0",
                          "bootstrap-user-software-key failed in w0"))


class PushingExecutor(FakeExecutor):
    """A VM world: the binaries under test went to three machines."""

    pushed = ["node1", "node2", "reflector"]


class TestPushedHeader(unittest.TestCase):
    def test_the_header_names_every_vm_the_binaries_reached(self):
        args = argparse.Namespace(selection=["bootstrap-user-software-key"],
                                  driver="agent", target="fresh", keep=False)
        with tempfile.TemporaryDirectory() as tmp, \
                mock.patch.object(runner, "RESULTS", Path(tmp)), \
                mock.patch.object(runner, "ensure_binary",
                                  return_value=(Path(tmp) / "astrald", "abc")), \
                mock.patch.object(runner, "_executor",
                                  lambda *_: PushingExecutor()), \
                contextlib.redirect_stdout(io.StringIO()):
            runner.main(args)
            run_dir = next(Path(tmp).iterdir())
            doc = json.loads((run_dir / "results.json").read_text())
        self.assertEqual(doc["pushed"], "node1,node2,reflector")


class TestDriverEnv(unittest.TestCase):
    AMBIENT = {"ASTRAL_ENDPOINT": "tcp:127.0.0.1:8625",
               "ASTRALD_APPHOST_TOKEN": "developer-token"}

    def test_a_stage_run_hands_its_drivers_a_stripped_env(self):
        # why through main(): the defect was the call site, which passed the
        # header's hermetic — false under stage: — and stripped nothing
        args = argparse.Namespace(selection=["smoke"], driver="script",
                                  target="stage:two-nodes", keep=False)
        seen = []

        def run_step(py, script, log, env, timeout):
            seen.append(env)
            return 0

        with tempfile.TemporaryDirectory() as tmp, \
                mock.patch.dict("os.environ", self.AMBIENT), \
                mock.patch.object(runner, "RESULTS", Path(tmp)), \
                mock.patch.object(runner, "ensure_binary",
                                  return_value=(Path(tmp) / "astrald", "abc")), \
                mock.patch.object(runner, "_executor",
                                  lambda *_: FakeExecutor()), \
                mock.patch.object(runner, "_run_step", run_step), \
                contextlib.redirect_stdout(io.StringIO()):
            self.assertEqual(runner.main(args), 0)
        self.assertEqual(len(seen), 2)              # driver, then oracle
        for env in seen:
            for var in self.AMBIENT:
                self.assertNotIn(var, env)

    def env_for(self, target: str) -> dict:
        with mock.patch.dict("os.environ", self.AMBIENT):
            return runner.driver_env(Path("/s/session.json"), target)

    def test_ambient_endpoint_and_token_are_stripped_but_under_attach(self):
        # why: a developer's token answered for a test's anonymous connect
        # under stage:, which stripped nothing because it is not hermetic
        for target in ("fresh", "stage:two-nodes"):
            with self.subTest(target=target):
                env = self.env_for(target)
                for var in self.AMBIENT:
                    self.assertNotIn(var, env)
        env = self.env_for("attach")
        for var, value in self.AMBIENT.items():
            self.assertEqual(env[var], value)
        self.assertEqual(env["ASTRAL_TESTS_SESSION"], "/s/session.json")


class TestPlanFor(unittest.TestCase):
    """Bare selections over the shipped manifests."""

    def setUp(self):
        self.all = load_all(TESTS / "e2e")

    def order(self, *selection) -> list:
        return [t.name for t, _ in runner.plan_for(self.all, list(selection))]

    def test_a_mutator_runs_after_the_test_it_would_break(self):
        # why: ordered by depth alone, expel-node ran first and mcp-peer met
        # a swarm with node2 already expelled
        order = self.order("expel-node", "mcp-peer")
        self.assertLess(order.index("mcp-peer"), order.index("expel-node"))
        self.assertEqual(order[-1], "expel-node")

    def test_an_observer_runs_before_a_producer_at_its_depth(self):
        # why: claim-window needs node1 unclaimed, and bootstrap claims it
        self.assertEqual(self.order("adopt-node", "claim-window"),
                         ["claim-window", "bootstrap-user-software-key",
                          "adopt-node"])

    def test_a_selection_holding_two_mutators_is_refused(self):
        with self.assertRaisesRegex(ValueError, "both mutate"):
            runner.plan_for(self.all, ["expel-node", "hold-purge"])


if __name__ == "__main__":
    unittest.main()
