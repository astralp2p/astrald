"""tests/summary over a results directory holding runs of every v2 shape."""
import contextlib
import importlib.machinery
import importlib.util
import io
import json
import tempfile
import unittest
from pathlib import Path
from unittest import mock

TESTS = Path(__file__).resolve().parent.parent


def load_summary():
    loader = importlib.machinery.SourceFileLoader("summary", str(TESTS / "summary"))
    spec = importlib.util.spec_from_loader("summary", loader)
    module = importlib.util.module_from_spec(spec)
    loader.exec_module(module)
    return module


def record(test, status, **kw):
    e = {"test": test, "kind": "test", "env": "node", "driver": "script",
         "status": status, "duration_s": 1.0, "artifacts": f"{test}/"}
    e.update(kw)
    return e


class TestSummary(unittest.TestCase):
    def test_runs_from_before_and_after_v2_2_mix(self):
        summary = load_summary()
        with tempfile.TemporaryDirectory() as tmp:
            runs = {
                # v2, before world/phase/reason
                "20261028T130351Z": [record("smoke", "pass"),
                                     record("adopt-node", "pass"),
                                     record("expel-node", "skipped")],
                # v2.2, an interrupted run
                "20261029T090000Z": [
                    record("smoke", "fail", failure_kind="verify",
                           world="w0", phase="verify"),
                    record("adopt-node", "skipped", world="w0",
                           phase="driver", reason="interrupted"),
                    record("expel-node", "skipped", world="w0",
                           phase="prepare", reason="interrupted")],
            }
            for stamp, entries in runs.items():
                d = Path(tmp) / stamp
                d.mkdir()
                doc = {"wall_time_s": 1.0, "entries": entries}
                if stamp.startswith("20261029"):
                    doc["aborted"] = "interrupted"
                (d / "results.json").write_text(json.dumps(doc))
            with mock.patch.object(summary, "RESULTS", Path(tmp)):
                seen = summary.collect(include_failures=True)
                with mock.patch("sys.argv", ["summary", "--all"]), \
                        contextlib.redirect_stdout(io.StringIO()):
                    self.assertEqual(summary.main(), 0)
        # the newest verdict of each test wins, whichever shape it has; an
        # interrupted run's skip hides no older verdict, and stands only
        # where no run reached one
        newest = {key[0]: (e["status"], e["stamp"]) for key, e in seen.items()}
        self.assertEqual(newest, {"smoke": ("fail", "20261029T090000Z"),
                                  "adopt-node": ("pass", "20261028T130351Z"),
                                  "expel-node": ("skipped",
                                                 "20261029T090000Z")})

    def test_a_test_row_outranks_its_replays_in_the_same_run(self):
        summary = load_summary()
        entries = [record("bootstrap", "fail", failure_kind="verify",
                          world="w0", phase="verify"),
                   record("bootstrap", "pass", kind="prereq", world="w1",
                          phase="verify", artifacts="bootstrap.w1/"),
                   record("adopt", "pass", kind="prereq", world="w1",
                          phase="verify")]
        with tempfile.TemporaryDirectory() as tmp:
            d = Path(tmp) / "20261029T100000Z"
            d.mkdir()
            (d / "results.json").write_text(json.dumps(
                {"wall_time_s": 1.0, "entries": entries}))
            with mock.patch.object(summary, "RESULTS", Path(tmp)):
                passes = summary.collect(include_failures=False)
                every = summary.collect(include_failures=True)
        # a replay's pass does not stand in for the test's own fail, and a
        # test the run only replayed still counts once
        self.assertEqual(sorted(k[0] for k in passes), ["adopt"])
        self.assertEqual({k[0]: e["status"] for k, e in every.items()},
                         {"bootstrap": "fail", "adopt": "pass"})


if __name__ == "__main__":
    unittest.main()
