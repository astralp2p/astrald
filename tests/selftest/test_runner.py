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


if __name__ == "__main__":
    unittest.main()
