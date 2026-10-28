"""The runner's per-test loop, driven against a fake executor."""
import argparse
import contextlib
import io
import json
import tempfile
import unittest
from pathlib import Path
from unittest import mock

from lib import runner
from lib.executors import ExecutorError


class FakeExecutor:
    """A world that comes up, and an operator that breaks before it drives."""

    def __init__(self, dir: Path):
        self.dir = Path(dir)
        self.dir.mkdir(parents=True, exist_ok=True)

    @property
    def session_json_path(self) -> Path:
        return self.dir / "session.json"

    def use_operator(self, profile: dict) -> None:
        pass

    def want_model(self, model: str, base_url: str = "") -> None:
        pass

    def prepare(self, test) -> None:
        pass

    def run_agent(self, test, log: Path, facts: Path, timeout: int) -> int:
        raise ExecutorError("node1: operator model is not m after the rewrite")

    def merge_facts(self, path: Path) -> None:
        pass

    def broken(self) -> list:
        return []

    def teardown(self, keep: bool) -> None:
        pass


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
                                  lambda args, plan, dir, *rest:
                                  FakeExecutor(dir)), \
                contextlib.redirect_stdout(io.StringIO()):
            self.assertEqual(runner.main(args), 1)
            run_dir = next(Path(tmp).iterdir())
            doc = json.loads((run_dir / "results.json").read_text())
            self.assertTrue((run_dir / "report.md").is_file())
            log = (run_dir / "bootstrap-user-software-key" / "driver.log")
            self.assertIn("[runner] node1: operator model is not m",
                          log.read_text())
        [e] = doc["entries"]
        self.assertEqual((e["test"], e["status"], e["failure_kind"]),
                         ("bootstrap-user-software-key", "fail", "environment"))


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
                                  lambda args, plan, dir, *rest:
                                  FakeExecutor(dir)), \
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


if __name__ == "__main__":
    unittest.main()
