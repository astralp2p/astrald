import tempfile
import textwrap
import unittest
from pathlib import Path

from lib.scenarios import load_all, resolve


def write_scenario(root, name, toml):
    d = Path(root) / name
    d.mkdir(parents=True)
    (d / "scenario.toml").write_text(textwrap.dedent(toml))


class TestScenarios(unittest.TestCase):
    def setUp(self):
        self.tmp = tempfile.TemporaryDirectory()
        root = self.tmp.name
        write_scenario(root, "smoke", """
            start = "null"
            nodes = ["node1"]
            drivers = ["script"]
            timeout = 60
        """)
        write_scenario(root, "bootstrap-user-software-key", """
            start = "null"
            saves = "one-node"
            nodes = ["node1"]
            drivers = ["script"]
            timeout = 120
        """)
        write_scenario(root, "adopt-node", """
            start = "one-node"
            saves = "two-nodes"
            nodes = ["node1", "node2"]
            drivers = ["script"]
            timeout = 180
        """)
        self.all = load_all(Path(root))

    def tearDown(self):
        self.tmp.cleanup()

    def test_load(self):
        self.assertEqual(set(self.all), {"smoke", "bootstrap-user-software-key",
                                         "adopt-node"})
        self.assertEqual(self.all["adopt-node"].start, "one-node")
        self.assertEqual(self.all["smoke"].saves, None)

    def test_full_run_order_and_kinds(self):
        plan = resolve(self.all, only=None)
        names = [s.name for s, _ in plan]
        self.assertEqual(names, ["bootstrap-user-software-key", "smoke",
                                 "adopt-node"])
        self.assertTrue(all(kind == "test" for _, kind in plan))

    def test_only_builds_fixture_prefix(self):
        plan = resolve(self.all, only=["adopt-node"])
        self.assertEqual([(s.name, k) for s, k in plan],
                         [("bootstrap-user-software-key", "fixture"),
                          ("adopt-node", "test")])

    def test_errors(self):
        with self.assertRaises(ValueError):
            resolve(self.all, only=["no-such"])
        bad = dict(self.all)
        bad["orphan"] = bad["adopt-node"].__class__(
            name="orphan", dir=Path("."), start="missing-state", saves=None,
            nodes=["node1"], drivers=["script"], timeout=1)
        with self.assertRaises(ValueError):
            resolve(bad, only=["orphan"])

    def test_unrelated_broken_scenario_does_not_break_selection(self):
        write_scenario(self.tmp.name, "broken", """
            start = "missing-state"
            nodes = ["node1"]
        """)
        all2 = load_all(Path(self.tmp.name))
        plan = resolve(all2, only=["smoke"])
        self.assertEqual([(s.name, k) for s, k in plan], [("smoke", "test")])
        with self.assertRaises(ValueError):
            resolve(all2, only=["broken"])

    def test_cycle_detected(self):
        write_scenario(self.tmp.name, "cyc-a", """
            start = "state-b"
            saves = "state-a"
            nodes = ["node1"]
        """)
        write_scenario(self.tmp.name, "cyc-b", """
            start = "state-a"
            saves = "state-b"
            nodes = ["node1"]
        """)
        with self.assertRaises(ValueError):
            resolve(load_all(Path(self.tmp.name)), only=["cyc-a"])


if __name__ == "__main__":
    unittest.main()
