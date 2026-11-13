"""A test's while_down.py: run once between a restart's stop and its start.

The runner is driven through runner.main against a daemon-free world whose
node1 is a loopback listener a restart closes and opens again. The world
writes `stop` and `start` into <root>/events, and every script of the test
writes there too, with whether node1's endpoint answered it and the facts
its view held, so the order is read from one file.
"""
import argparse
import contextlib
import io
import json
import socket
import tempfile
import unittest
from pathlib import Path
from types import SimpleNamespace
from unittest import mock

from lib import manifest, runner
from lib.worlds import World, node_entry
from lib.worlds.local import LocalWorld
from lib.nodeconfig import PortSpan

# One body for every script: note whether node1 answers and the facts the
# view holds; a driver writes one fact named after its test. A script exits
# with the code in <phase>.exit when its test directory holds one.
STEP = '''\
import json, os, socket, sys
from pathlib import Path
here = Path(__file__).parent
phase = Path(__file__).stem
view = json.loads(Path(os.environ["ASTRAL_TESTS_SESSION"]).read_text())
node = view["nodes"]["node1"]
host, port = node["endpoint"].removeprefix("tcp:").rsplit(":", 1)
try:
    socket.create_connection((host, int(port)), timeout=5).close()
    reached = "answers"
except ConnectionRefusedError:
    reached = "refuses"
with open(Path(node["root"]) / "events", "a") as f:
    f.write(f"{phase} {reached} {json.dumps(view['facts'], sort_keys=True)}\\n")
if phase == "script":
    Path(os.environ["ASTRAL_TESTS_FACTS_OUT"]).write_text(
        json.dumps({here.name: 1}))
code = here / f"{phase}.exit"
sys.exit(int(code.read_text()) if code.exists() else 0)
'''

# name -> (after, ships while_down.py, while_down.py's exit code)
TREE = {
    "base":       (None, False, 0),
    "down":       ("base", True, 0),
    "plain":      ("base", False, 0),
    "down-fails": ("base", True, 1),
}


def _listen(port: int) -> socket.socket:
    s = socket.socket()
    s.setsockopt(socket.SOL_SOCKET, socket.SO_REUSEADDR, 1)
    s.bind(("127.0.0.1", port))
    s.listen()
    return s


class ListeningWorld(World):
    """node1 is a listener on loopback: up while the node runs, closed
    while a restart has it stopped."""

    pushed = ()

    def __init__(self, root: Path):
        self.root = root
        self.restarts = 0
        self.sock = _listen(0)

    def _note(self, line: str) -> None:
        with (self.root / "events").open("a") as f:
            f.write(line + "\n")

    def ensure(self, test) -> None:
        pass

    def restart(self, names: list, while_down=None) -> None:
        port = self.sock.getsockname()[1]
        self.sock.close()
        self._note("stop")
        if while_down:
            while_down()
        self.sock = _listen(port)
        self._note("start")
        self.restarts += 1

    def nodes(self) -> dict:
        port = self.sock.getsockname()[1]
        return {"node1": node_entry(endpoint=f"tcp:127.0.0.1:{port}",
                                    token="t", identity="id",
                                    root=str(self.root),
                                    restarts=self.restarts)}

    def dead(self) -> list:
        return []

    def close(self, keep: bool) -> None:
        self.sock.close()


def write_tree(root: Path) -> dict:
    for name, (after, ships, code) in TREE.items():
        d = root / name
        d.mkdir(parents=True)
        lines = ['nodes = ["node1"]']
        if after:
            lines += [f'after = "{after}"', 'restart = ["node1"]']
        (d / "test.toml").write_text("\n".join(lines) + "\n")
        (d / "script.py").write_text(STEP)
        (d / "verify.py").write_text(STEP)
        if ships:
            (d / "while_down.py").write_text(STEP)
        if code:
            (d / "while_down.exit").write_text(str(code))
    return manifest.load_all(root)


def run(test: str) -> tuple:
    """(events, results entry of `test`, its artifact dir) of a run of
    `test` with its prereqs."""
    tmp = Path(tempfile.mkdtemp())
    tests = write_tree(tmp / "e2e")
    (tmp / "node1").mkdir()
    ex = ListeningWorld(tmp / "node1")
    args = argparse.Namespace(selection=[test], driver="script",
                              target="fresh", keep=False)
    with mock.patch.object(runner, "RESULTS", tmp / "results"), \
            mock.patch.object(runner.manifest, "load_all", lambda _: tests), \
            mock.patch.object(runner, "ensure_binary",
                              return_value=(tmp / "astrald", "abc")), \
            mock.patch.object(runner, "_local_world", lambda *_: ex), \
            mock.patch.object(runner, "lease_ports",
                              return_value=SimpleNamespace(base=0)), \
            contextlib.redirect_stdout(io.StringIO()), \
            contextlib.redirect_stderr(io.StringIO()):
        runner.main(args)
    run_dir = next((tmp / "results").iterdir())
    doc = json.loads((run_dir / "results.json").read_text())
    [entry] = [e for e in doc["entries"] if e["test"] == test]
    events = (tmp / "node1" / "events").read_text().splitlines()
    return events, entry, run_dir / test


# what base, the prereq, leaves in the events before its child runs
BASE = ['script answers {}', 'verify answers {"base": 1}',
        'script answers {"base": 1}']


class TestWhileDown(unittest.TestCase):
    def test_the_script_runs_once_between_stop_and_start_on_a_down_node(self):
        events, entry, art = run("down")
        self.assertEqual(events, BASE + [
            "stop",
            'while_down refuses {"base": 1, "down": 1}',
            "start",
            'verify answers {"base": 1, "down": 1}'])
        self.assertEqual(entry["status"], "pass")
        self.assertTrue((art / "while_down.log").is_file())

    def test_a_non_zero_exit_is_a_driver_failure_and_the_node_starts(self):
        # why the node starts: every other restart leaves the world up, and
        # a node left down reads as a dead one to the liveness check
        events, entry, art = run("down-fails")
        self.assertEqual(events, BASE + [
            "stop",
            'while_down refuses {"base": 1, "down-fails": 1}',
            "start"])
        self.assertEqual((entry["status"], entry["failure_kind"],
                          entry["phase"]), ("fail", "driver", "restart"))
        self.assertIn("[runner] while_down.py exited 1",
                      (art / "driver.log").read_text())

    def test_a_test_without_the_script_restarts_as_before(self):
        events, entry, art = run("plain")
        self.assertEqual(events, BASE + [
            "stop", "start", 'verify answers {"base": 1, "plain": 1}'])
        self.assertEqual(entry["status"], "pass")
        self.assertFalse((art / "while_down.log").exists())

    def test_a_world_that_never_calls_the_hook_is_no_pass(self):
        # why: a restart that skipped the script would judge a node whose
        # changes were never made
        class Skips(ListeningWorld):
            def restart(self, names, while_down=None):
                super().restart(names)

        with mock.patch(f"{__name__}.ListeningWorld", Skips):
            _, entry, _ = run("down")
        self.assertEqual((entry["status"], entry["failure_kind"],
                          entry["phase"]), ("fail", "environment", "restart"))


class FakeNode:
    def __init__(self, name: str, events: list):
        self.name, self.events, self.restarts = name, events, 0

    def stop(self):
        self.events.append(("stop", self.name))

    def start(self, keep):
        self.events.append(("start", self.name))

    async def wait_ready(self):
        pass


class TestLocalWorld(unittest.TestCase):
    def test_every_node_stops_before_the_hook_and_starts_after_it(self):
        tmp = Path(tempfile.mkdtemp())
        w = LocalWorld(tmp / "w0", tmp / "astrald", PortSpan(20800, 2))
        self.addCleanup(w.close, False)
        events = []
        w._nodes = {n: FakeNode(n, events) for n in ("node1", "node2")}
        w.restart(["node1", "node2"], lambda: events.append(("hook",)))
        self.assertEqual(events, [("stop", "node1"), ("stop", "node2"),
                                  ("hook",),
                                  ("start", "node1"), ("start", "node2")])
        self.assertEqual([n.restarts for n in w._nodes.values()], [1, 1])


if __name__ == "__main__":
    unittest.main()
