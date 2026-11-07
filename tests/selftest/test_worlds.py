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
from lib.worlds import NODE_FIELDS, WorldError
from lib.worlds.attach import AttachWorld
from lib.nodeconfig import PortSpan
from lib.worlds.local import LocalWorld
from selftest.test_world_record import FakeWorld, write_tree


class FailsToClose(FakeWorld):
    def close(self, keep: bool) -> None:
        raise WorldError("close: node1: did not stop")


class DiesAfterBuild(FakeWorld):
    """A world whose node dies under `build`."""

    def __init__(self):
        super().__init__()
        self.prepared = []

    def ensure(self, test) -> None:
        self.prepared.append(test.name)

    def dead(self) -> list:
        return ["node1"] if "build" in self.prepared else []


def run_worlds(tmp: Path, selection: list, world=FakeWorld,
               isolate=False, config=None) -> tuple:
    """(run dir, results doc, the worlds opened) of one run of the tree;
    config gives tests their [config.*] overrides."""
    tests = write_tree(tmp / "e2e")
    for name, overrides in (config or {}).items():
        tests[name].config = overrides
    args = argparse.Namespace(selection=selection, driver="script",
                              target="fresh", keep=False, isolate=isolate)
    opened = []

    def open_world(dir, binary, span, keep, config=None):
        opened.append((Path(dir).parent.name, world()))
        opened[-1][1].config = config
        opened[-1][1].max_nodes = span.max_nodes
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
                                     PortSpan(20800, 20), config={"nomcp1": {
                                         "mcp": {"bind_mcp": ""}}})
            try:
                world.ensure(SimpleNamespace(name="t", ops=[],
                                              nodes=["node1", "nomcp1"]))
                views = world.nodes()
            finally:
                world.close(keep=False)
        self.assertEqual(started["nomcp1"].configs["mcp"]["bind_mcp"], "")
        self.assertEqual(started["node1"].configs["mcp"]["bind_mcp"],
                         "tcp:127.0.0.1:20804")
        self.assertEqual((views["node1"]["serves_mcp"],
                          views["nomcp1"]["serves_mcp"]), (True, False))


class StartsNothing:
    """A LocalNode that records its start and stop and runs no daemon."""
    started: list = []

    def __init__(self, name, root, binary, ports, configs=None):
        self.name, self.ports, self.stopped = name, ports, False

    def start(self, keep=False):
        StartsNothing.started.append(self.name)

    async def wait_ready(self, timeout=30.0):
        pass

    def stop(self):
        self.stopped = True


class TestLocalWorldSpan(unittest.TestCase):
    def world(self, tmp, max_nodes):
        StartsNothing.started = []
        return LocalWorld(Path(tmp) / "session", Path("astrald"),
                          PortSpan(20800, max_nodes))

    def ensure(self, world, *nodes):
        world.ensure(SimpleNamespace(name="t", ops=[], nodes=list(nodes)))

    def test_a_roster_past_the_limit_starts_none_of_its_new_nodes(self):
        from lib.worlds import local
        with tempfile.TemporaryDirectory() as tmp, \
                mock.patch.object(local, "LocalNode", StartsNothing):
            world = self.world(tmp, 2)
            self.ensure(world, "node1")
            with self.assertRaisesRegex(
                    WorldError, r"^session: node2, node3: the world already "
                    r"runs 1 nodes, and \[ports\] span in tests/config.toml "
                    r"gives a world 2$"):
                self.ensure(world, "node1", "node2", "node3")
            self.assertEqual(StartsNothing.started, ["node1"])
            self.assertEqual(list(world._nodes), ["node1"])
            self.assertFalse(world._nodes["node1"].stopped)
            world.close(keep=False)

    def test_a_roster_of_nodes_already_up_is_never_refused(self):
        from lib.worlds import local
        with tempfile.TemporaryDirectory() as tmp, \
                mock.patch.object(local, "LocalNode", StartsNothing):
            world = self.world(tmp, 2)
            self.ensure(world, "node1", "node2", "node1")
            self.ensure(world, "node2", "node1")
            self.assertEqual(StartsNothing.started, ["node1", "node2"])
            self.assertEqual(world._nodes["node2"].ports.apphost, 20810)
            world.close(keep=False)

    def test_the_runner_gives_each_local_world_the_span_over_ten(self):
        import tomllib
        from lib.nodeconfig import PORTS_PER_NODE
        span = tomllib.loads((runner.TESTS / "config.toml")
                             .read_text())["ports"]["span"]
        with tempfile.TemporaryDirectory() as tmp:
            _, _, opened = run_worlds(Path(tmp), ["observer", "reader"],
                                      isolate=True)
        self.assertEqual({w.max_nodes for _, w in opened},
                         {span // PORTS_PER_NODE})


class StopsForGood(StartsNothing):
    """A StartsNothing that a stop leaves dead and that a view can read."""

    def __init__(self, name, root, binary, ports, configs=None):
        super().__init__(name, root, binary, ports, configs)
        self.endpoint, self.token, self.identity = f"tcp:{name}", "t", name
        self.root, self.serves_mcp, self.restarts = root, True, 0

    def alive(self):
        return not self.stopped


class TestLocalWorldStop(unittest.TestCase):
    def stopped_world(self, tmp):
        from lib.worlds import local
        StartsNothing.started = []
        world = LocalWorld(Path(tmp) / "session", Path("astrald"),
                           PortSpan(20800, 20))
        with mock.patch.object(local, "LocalNode", StopsForGood):
            world.ensure(SimpleNamespace(name="t", ops=[],
                                         nodes=["first", "member"]))
        world.stop(["first"])
        return world

    def test_a_stopped_node_is_neither_in_the_view_nor_dead(self):
        with tempfile.TemporaryDirectory() as tmp:
            world = self.stopped_world(tmp)
            self.assertTrue(world._nodes["first"].stopped)
            self.assertEqual(list(world.nodes()), ["member"])
            self.assertEqual(world.dead(), [])
            world._nodes["member"].stopped = True
            self.assertEqual(world.dead(), ["member"])
            world.close(keep=False)

    def test_a_later_roster_or_restart_naming_a_stopped_node_is_refused(self):
        with tempfile.TemporaryDirectory() as tmp:
            world = self.stopped_world(tmp)
            with self.assertRaisesRegex(
                    WorldError, "^later: first was stopped earlier in this "
                    "world$"):
                world.ensure(SimpleNamespace(name="later", ops=[],
                                             nodes=["member", "first"]))
            with self.assertRaisesRegex(WorldError, "^restart: first was "
                                        "stopped"):
                world.restart(["first"])
            self.assertEqual(StartsNothing.started, ["first", "member"])
            world.close(keep=False)


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

            def dead(self) -> list:
                return super().dead() if self.first else []

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
                         "WorldError: close: node1: did not stop")

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
            world = LocalWorld(dir, Path(tmp) / "astrald", PortSpan(0, 20))
            nodes = {"node1": Node(True), "node2": Node(False)}
            world._nodes = nodes
            with self.assertRaisesRegex(WorldError, "node1: still running"):
                world.close(keep=False)
            self.assertTrue(dir.is_dir() and not dir.is_symlink())
        self.assertTrue(all(n.stopped for n in nodes.values()))


class TestAttachNodes(unittest.TestCase):
    def test_a_field_attach_cannot_provide_is_none(self):
        # why: a sentinel reads as a value — root "attached" as a path, and
        # tcp_port 0 as the address tcp:127.0.0.1:0
        world = AttachWorld()
        world._node = {"name": "node1", "endpoint": "tcp:127.0.0.1:8625",
                       "token": "t", "identity": "id"}
        node = world.nodes()["node1"]
        self.assertEqual({f for f in NODE_FIELDS if node[f] is not None},
                         {"endpoint", "token", "identity"})


if __name__ == "__main__":
    unittest.main()
