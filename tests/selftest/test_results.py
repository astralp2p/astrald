import json
import tempfile
import unittest
from pathlib import Path

from lib.results import RunResults


class TestResults(unittest.TestCase):
    def make(self, tmp):
        return RunResults(Path(tmp), astrald_ref="abc1234", host="pi",
                          sandbox="host", lanes=["node"])

    def test_pass_run_exits_zero_and_writes_files(self):
        with tempfile.TemporaryDirectory() as tmp:
            r = self.make(tmp)
            r.record(test="node/smoke", kind="test", lane="node", driver="script",
                     status="pass", duration_s=1.2, artifacts="node/smoke/")
            self.assertEqual(r.finalize(), 0)
            data = json.loads((Path(tmp) / "results.json").read_text())
            self.assertEqual(data["astrald_ref"], "abc1234")
            self.assertEqual(data["sandbox"], "host")
            self.assertEqual(len(data["entries"]), 1)
            self.assertEqual(data["entries"][0]["status"], "pass")
            lines = (Path(tmp) / "events.jsonl").read_text().splitlines()
            self.assertEqual(json.loads(lines[0])["test"], "node/smoke")

    def test_fail_and_skip_exit_nonzero(self):
        with tempfile.TemporaryDirectory() as tmp:
            r = self.make(tmp)
            r.record(test="node/bootstrap-user-software-key", kind="fixture",
                     lane="node", driver="script", status="fail",
                     failure_kind="verify", duration_s=3.0,
                     artifacts="node/bootstrap-user-software-key/")
            r.record(test="node/adopt-node", kind="test", lane="node",
                     driver="script", status="skipped", duration_s=0.0,
                     artifacts="node/adopt-node/")
            self.assertEqual(r.finalize(), 1)

    def test_validation(self):
        with tempfile.TemporaryDirectory() as tmp:
            r = self.make(tmp)
            with self.assertRaises(ValueError):
                r.record(test="x", kind="test", lane="node", driver="script",
                         status="nope", duration_s=0, artifacts="x/")
            with self.assertRaises(ValueError):   # fail requires failure_kind
                r.record(test="x", kind="test", lane="node", driver="script",
                         status="fail", duration_s=0, artifacts="x/")


if __name__ == "__main__":
    unittest.main()
