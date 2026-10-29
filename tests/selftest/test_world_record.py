"""Which facts a test sees, and where the runner writes what it sees.

A synthetic tree runs through the real runner against a fake executor. Every
driver and oracle copies the session view it was handed beside its logs, so
the assertions read exactly what a real test would have read.
"""
import argparse
import contextlib
import io
import json
import tempfile
import unittest
from pathlib import Path
from types import SimpleNamespace
from unittest import mock

from lib import manifest, runner
from lib.executors import NODE_FIELDS, ExecutorError, node_entry
from lib.record import WorldRecord

# One body for every synthetic driver and oracle: copy the view it was
# handed, with the stamp fs-watch derives from the view's path, and, as a
# driver, write one fact named after the test. An oracle fails when the
# test directory holds a FAIL marker.
STEP = '''\
import json, os, sys
from pathlib import Path
name, phase = Path(__file__).parent.name, Path(__file__).stem
session = Path(os.environ["ASTRAL_TESTS_SESSION"])
seen = json.loads(session.read_text())
seen["stamp"] = session.resolve().parent.parent.name
(session.parent / f"seen-{phase}.json").write_text(json.dumps(seen))
if phase == "script":
    Path(os.environ["ASTRAL_TESTS_FACTS_OUT"]).write_text(
        json.dumps({name: phase}))
sys.exit(1 if (Path(__file__).parent / "FAIL").exists() else 0)
'''

# name -> (start, saves, oracle fails, restarts node1)
TREE = {
    "build":        ("null", "one", False, False),
    "observer":     ("one", None, False, False),
    "reader":       ("one", None, False, True),
    "broken":       ("one", "two", True, False),
    "after-broken": ("two", None, False, False),
}


class FakeExecutor:
    pushed = ()

    def __init__(self):
        self.restarts = 0

    def prepare(self, test) -> None:
        pass

    def restart(self, names: list) -> None:
        self.restarts += 1

    def nodes(self) -> dict:
        return {"node1": node_entry(endpoint="tcp:127.0.0.1:1",
                                    token="t", identity="id",
                                    restarts=self.restarts)}

    def broken(self) -> list:
        return []

    def save(self, state: str) -> None:
        pass

    def teardown(self, keep: bool) -> None:
        pass


def write_tree(root: Path) -> dict:
    producers = {saves: name for name, (_, saves, _, _) in TREE.items()
                 if saves}
    for name, (start, saves, fails, restarts) in TREE.items():
        d = root / name
        d.mkdir(parents=True)
        lines = [f'env = "node"', f'start = "{start}"', 'nodes = ["node1"]']
        if saves:
            lines.append(f'saves = "{saves}"')
        if start != "null":
            lines.append(f'after = "{producers[start]}"')
        if restarts:
            lines.append('restart = ["node1"]')
        (d / "test.toml").write_text("\n".join(lines) + "\n")
        (d / "script.py").write_text(STEP)
        (d / "verify.py").write_text(STEP)
        if fails:
            (d / "FAIL").write_text("")
    return manifest.load_all(root)


def run_tree(tmp: Path, selection: list, ex) -> Path:
    """Run `selection` of the synthetic tree against `ex`; its run directory."""
    tests = write_tree(tmp / "e2e")
    args = argparse.Namespace(selection=selection, driver="script",
                              target="fresh", keep=False)
    with mock.patch.object(runner, "RESULTS", tmp / "results"), \
            mock.patch.object(runner.manifest, "load_all", lambda _: tests), \
            mock.patch.object(runner, "ensure_binary",
                              return_value=(tmp / "astrald", "abc")), \
            mock.patch.object(runner, "_executor", lambda *_: ex), \
            mock.patch.object(runner, "_local_world", lambda *_: ex), \
            mock.patch.object(runner, "lease_ports",
                              return_value=SimpleNamespace(base=0)), \
            contextlib.redirect_stdout(io.StringIO()), \
            contextlib.redirect_stderr(io.StringIO()):
        runner.main(args)
    return next((tmp / "results").iterdir())


class TestFactVisibility(unittest.TestCase):
    @classmethod
    def setUpClass(cls):
        cls._tmp = tempfile.TemporaryDirectory()
        cls.run_dir = run_tree(Path(cls._tmp.name), list(TREE), FakeExecutor())
        cls.world = json.loads((cls.run_dir / "w0" / "world.json").read_text())

    @classmethod
    def tearDownClass(cls):
        cls._tmp.cleanup()

    def seen(self, test: str, phase: str) -> dict:
        return json.loads((self.run_dir / test / f"seen-{phase}.json")
                          .read_text())

    def test_a_driver_sees_its_lineage_and_not_a_sibling(self):
        # why: observer ran and passed before reader, from the same start;
        # with one flat dict its facts reached reader
        self.assertEqual(self.seen("reader", "script")["facts"],
                         {"build": "script"})

    def test_an_oracle_also_sees_its_own_drivers_facts(self):
        self.assertEqual(self.seen("reader", "verify")["facts"],
                         {"build": "script", "reader": "script"})

    def test_a_failed_test_adds_no_facts(self):
        # why: facts were merged before the oracle, so a rejected flow's
        # facts reached every later test
        self.assertNotIn("broken", self.world["facts"])
        self.assertEqual(sorted(self.world["facts"]),
                         ["build", "observer", "reader"])

    def test_the_record_holds_every_test_that_ran_in_order(self):
        # note: after-broken is skipped, so it never touched the world
        self.assertEqual(
            [(h["test"], h["status"]) for h in self.world["history"]],
            [("build", "pass"), ("observer", "pass"), ("reader", "pass"),
             ("broken", "fail")])

    def test_the_view_names_its_run_by_content_and_by_path(self):
        # why: fs-watch and discover-swarm take a run-unique stamp from the
        # view's resolved path, and a temp-dir symlink made it read `tmp`
        seen = self.seen("reader", "script")
        self.assertEqual(seen["run"], self.run_dir.name)
        self.assertEqual(seen["stamp"], self.run_dir.name)

    def test_every_node_carries_every_field(self):
        node = self.seen("build", "script")["nodes"]["node1"]
        self.assertEqual(tuple(node), NODE_FIELDS)
        self.assertIsNone(node["mcp_url"])

    def test_the_oracle_reads_the_nodes_after_the_restart(self):
        # why: the view is written again before the oracle so it judges the
        # node the restart brought back, and can tell the restart happened
        restarts = [self.seen("reader", phase)["nodes"]["node1"]["restarts"]
                    for phase in ("script", "verify")]
        self.assertEqual(restarts, [0, 1])

    def test_the_record_names_the_nodes_the_world_ended_with(self):
        self.assertEqual(self.world["nodes"]["node1"]["restarts"], 1)


class TestPrereqFacts(unittest.TestCase):
    def test_a_driver_sees_the_facts_of_a_prereq_it_stands_on(self):
        # why: a record that kept only kind == "test" facts would hand a
        # bare selection none of what its prereq prefix learned
        with tempfile.TemporaryDirectory() as tmp:
            run_dir = run_tree(Path(tmp), ["reader"], FakeExecutor())
            kinds = [e["kind"] for e in json.loads(
                (run_dir / "results.json").read_text())["entries"]]
            seen = json.loads((run_dir / "reader" / "seen-script.json")
                              .read_text())
        self.assertEqual(kinds, ["prereq", "test"])
        self.assertEqual(seen["facts"], {"build": "script"})


class UnpreparedExecutor(FakeExecutor):
    """A world whose node came up, and whose first prepare then broke."""

    def prepare(self, test) -> None:
        raise ExecutorError("node1: not ready after 30s")


class TestRecordWithoutAView(unittest.TestCase):
    def test_a_world_whose_first_prepare_failed_still_names_its_nodes(self):
        # why: --keep is for exactly this run, and no view was written to
        # carry the nodes into the record
        with tempfile.TemporaryDirectory() as tmp:
            run_dir = run_tree(Path(tmp), ["build"], UnpreparedExecutor())
            world = json.loads((run_dir / "w0" / "world.json").read_text())
        self.assertEqual(list(world["nodes"]), ["node1"])
        self.assertEqual(world["history"],
                         [{"test": "build", "kind": "test", "status": "fail"}])


class TestWorldRecord(unittest.TestCase):
    def test_the_nearest_ancestor_wins_a_shared_fact_name(self):
        with tempfile.TemporaryDirectory() as tmp:
            record = WorldRecord(Path(tmp))
            record.add("bootstrap", "test", "pass", {"user": "old"})
            record.add("adopt", "test", "pass", {"user": "new"})
            self.assertEqual(record.facts_of(["adopt", "bootstrap"]),
                             {"user": "new"})


if __name__ == "__main__":
    unittest.main()
