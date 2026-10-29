"""A run of several planned worlds, through the real runner.

The synthetic tree of test_world_record runs isolated or shared against fake
worlds, one per planned world, so the assertions read what each world's
records, views and logs really hold.
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

from lib import runner
from lib.executors import ExecutorError
from lib.worlds.local import LocalWorld
from selftest.test_world_record import FakeExecutor, write_tree


class FailsToClose(FakeExecutor):
    def teardown(self, keep: bool) -> None:
        raise ExecutorError("close: node1: did not stop")


class DiesAfterBuild(FakeExecutor):
    """A world whose node dies under `build`."""

    def __init__(self):
        super().__init__()
        self.prepared = []

    def prepare(self, test) -> None:
        self.prepared.append(test.name)

    def broken(self) -> list:
        return ["node1"] if "build" in self.prepared else []


def run_worlds(tmp: Path, selection: list, world=FakeExecutor,
               isolate=False, config=None) -> tuple:
    """(run dir, results doc, the worlds opened) of one run of the tree;
    config gives tests their [config.*] overrides."""
    tests = write_tree(tmp / "e2e")
    for name, overrides in (config or {}).items():
        tests[name].config = overrides
    args = argparse.Namespace(selection=selection, driver="script",
                              target="fresh", keep=False, isolate=isolate)
    opened = []

    def open_world(dir, binary, base, keep, config=None):
        opened.append((Path(dir).parent.name, world()))
        opened[-1][1].config = config
        return opened[-1][1]

    with mock.patch.object(runner, "RESULTS", tmp / "results"), \
            mock.patch.object(runner.manifest, "load_all", lambda _: tests), \
            mock.patch.object(runner, "ensure_binary",
                              return_value=(tmp / "astrald", "abc")), \
            mock.patch.object(runner, "_local_world", open_world), \
            mock.patch.object(runner, "lease_ports",
                              return_value=SimpleNamespace(base=0)), \
            contextlib.redirect_stdout(io.StringIO()), \
            contextlib.redirect_stderr(io.StringIO()):
        runner.main(args)
    run_dir = next((tmp / "results").iterdir())
    return run_dir, json.loads((run_dir / "results.json").read_text()), opened


def rows(doc: dict) -> list:
    return [(e["world"], e["test"], e["kind"], e["status"], e["artifacts"])
            for e in doc["entries"]]


class TestIsolatedWorlds(unittest.TestCase):
    @classmethod
    def setUpClass(cls):
        cls._tmp = tempfile.TemporaryDirectory()
        cls.run_dir, cls.doc, cls.opened = run_worlds(
            Path(cls._tmp.name), ["reader", "observer"], isolate=True)

    @classmethod
    def tearDownClass(cls):
        cls._tmp.cleanup()

    def test_each_test_runs_in_a_world_of_its_own(self):
        self.assertEqual([name for name, _ in self.opened], ["w0", "w1"])
        self.assertEqual(rows(self.doc), [
            ("w0", "build", "prereq", "pass", "build/"),
            ("w0", "observer", "test", "pass", "observer/"),
            ("w1", "build", "prereq", "pass", "build.w1/"),
            ("w1", "reader", "test", "pass", "reader/")])

    def test_a_replay_keeps_the_stamp_its_view_path_names(self):
        # why: fs-watch and discover-swarm read the run's stamp two levels
        # above their view, and a replay's directory must not change that
        seen = json.loads((self.run_dir / "build.w1" / "seen-script.json")
                          .read_text())
        self.assertEqual(seen["stamp"], self.run_dir.name)

    def test_each_world_keeps_its_own_record(self):
        for name, ran in (("w0", ["build", "observer"]),
                          ("w1", ["build", "reader"])):
            with self.subTest(world=name):
                world = json.loads((self.run_dir / name / "world.json")
                                   .read_text())
                self.assertEqual([h["test"] for h in world["history"]], ran)
                self.assertEqual(sorted(world["facts"]), sorted(ran))

    def test_a_replay_serves_the_facts_of_its_own_world(self):
        seen = json.loads((self.run_dir / "reader" / "seen-script.json")
                          .read_text())
        self.assertEqual(seen["facts"], {"build": "script"})


class TestWorldConfig(unittest.TestCase):
    def test_each_world_opens_with_its_own_merged_overrides(self):
        off = {"node1": {"mcp": {"bind_mcp": ""}}}
        on = {"node1": {"mcp": {"bind_mcp": "tcp:127.0.0.1:1"}}}
        with tempfile.TemporaryDirectory() as tmp:
            _, doc, opened = run_worlds(Path(tmp), ["observer", "reader"],
                                        config={"observer": off,
                                                "reader": on})
        # the clash forks reader into a world of its own, replaying build
        self.assertEqual([(name, w.config) for name, w in opened],
                         [("w0", off), ("w1", on)])
        self.assertEqual([e["status"] for e in doc["entries"]],
                         ["pass"] * 4)


class TestLocalWorldConfig(unittest.TestCase):
    def test_a_node_starts_with_its_worlds_overrides_for_it(self):
        from lib.localnode import LocalNode
        from lib.worlds import local

        started = {}

        class Node(LocalNode):
            def start(self, keep=False):
                started[self.name] = self

            async def wait_ready(self, timeout=30.0):
                pass

        with tempfile.TemporaryDirectory() as tmp, \
                mock.patch.object(local, "LocalNode", Node):
            world = local.LocalWorld(Path(tmp) / "session", Path("astrald"),
                                     20800, config={"nomcp1": {
                                         "mcp": {"bind_mcp": ""}}})
            try:
                world.prepare(SimpleNamespace(name="t", ops=[],
                                              nodes=["node1", "nomcp1"]))
                views = world.nodes()
            finally:
                world.teardown(keep=False)
        self.assertEqual(started["nomcp1"].configs["mcp"]["bind_mcp"], "")
        self.assertEqual(started["node1"].configs["mcp"]["bind_mcp"],
                         "tcp:127.0.0.1:20804")
        self.assertEqual((views["node1"]["serves_mcp"],
                          views["nomcp1"]["serves_mcp"]), (True, False))


class TestSkips(unittest.TestCase):
    def test_a_failed_lineage_skips_only_inside_its_world(self):
        with tempfile.TemporaryDirectory() as tmp:
            _, doc, _ = run_worlds(Path(tmp), ["after-broken", "observer"],
                                   isolate=True)
        self.assertEqual(
            [(e["world"], e["test"], e["status"], e.get("reason"))
             for e in doc["entries"]],
            [("w0", "build", "pass", None),
             ("w0", "observer", "pass", None),
             ("w1", "build", "pass", None),
             ("w1", "broken", "fail", None),
             ("w1", "after-broken", "skipped", "broken failed in w1")])

    def test_a_dead_world_skips_the_rest_of_itself(self):
        with tempfile.TemporaryDirectory() as tmp:
            _, doc, _ = run_worlds(Path(tmp), ["observer", "reader"],
                                   world=DiesAfterBuild)
        self.assertEqual(
            [(e["test"], e["status"], e.get("phase"), e.get("reason"))
             for e in doc["entries"]],
            [("build", "fail", "alive", None),
             ("observer", "skipped", "prepare", "world w0 died at build"),
             ("reader", "skipped", "prepare", "world w0 died at build")])

    def test_a_dead_world_leaves_the_next_world_alone(self):
        class DiesInW0(DiesAfterBuild):
            count = 0

            def __init__(self):
                super().__init__()
                DiesInW0.count += 1
                self.first = DiesInW0.count == 1

            def broken(self) -> list:
                return super().broken() if self.first else []

        with tempfile.TemporaryDirectory() as tmp:
            _, doc, _ = run_worlds(Path(tmp), ["observer", "reader"],
                                   world=DiesInW0, isolate=True)
        self.assertEqual([(e["world"], e["test"], e["status"])
                          for e in doc["entries"]],
                         [("w0", "build", "fail"),
                          ("w0", "observer", "skipped"),
                          ("w1", "build", "pass"),
                          ("w1", "reader", "pass")])


class TestClose(unittest.TestCase):
    def test_a_world_that_failed_to_close_skips_the_worlds_after_it(self):
        # why: its nodes may still hold the run's ports, and the next world
        # would boot onto them
        with tempfile.TemporaryDirectory() as tmp:
            _, doc, opened = run_worlds(Path(tmp), ["observer", "reader"],
                                        world=FailsToClose, isolate=True)
        self.assertEqual([name for name, _ in opened], ["w0"])
        self.assertEqual(
            [(e["world"], e["test"], e["status"], e.get("reason"))
             for e in doc["entries"]],
            [("w0", "build", "pass", None),
             ("w0", "observer", "pass", None),
             ("w1", "build", "skipped", "w0 failed to close"),
             ("w1", "reader", "skipped", "w0 failed to close")])
        self.assertEqual(doc["aborted"], "w0 failed to close; teardown: "
                         "ExecutorError: close: node1: did not stop")

    def test_close_stops_every_node_when_one_will_not(self):
        class Node:
            def __init__(self, fails):
                self.fails, self.stopped = fails, False

            def stop(self):
                self.stopped = True
                if self.fails:
                    raise OSError("still running")

        with tempfile.TemporaryDirectory() as tmp:
            dir = Path(tmp) / "w0" / "session"
            world = LocalWorld(dir, Path(tmp) / "astrald", 0)
            nodes = {"node1": Node(True), "node2": Node(False)}
            world._nodes = nodes
            with self.assertRaisesRegex(ExecutorError, "node1: still running"):
                world.teardown(keep=False)
            self.assertTrue(dir.is_dir() and not dir.is_symlink())
        self.assertTrue(all(n.stopped for n in nodes.values()))


if __name__ == "__main__":
    unittest.main()
