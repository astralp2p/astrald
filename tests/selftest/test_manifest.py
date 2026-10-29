import shutil
import tempfile
import textwrap
import unittest
from pathlib import Path

from lib.manifest import load_all, resolve, validate_order

CHAIN = {
    "smoke": """
        env = "node"
        start = "null"
        nodes = ["node1"]
        drivers = ["script"]
        timeout = 60
    """,
    "bootstrap-user-software-key": """
        env = "node"
        start = "null"
        saves = "one-node"
        nodes = ["node1"]
        drivers = ["script"]
        timeout = 120
    """,
    "adopt-node": """
        env = "node"
        start = "one-node"
        saves = "two-nodes"
        nodes = ["node1", "node2"]
        drivers = ["script"]
        timeout = 180
    """,
}


def write_test(root, name, toml):
    d = Path(root) / name
    d.mkdir(parents=True, exist_ok=True)
    (d / "test.toml").write_text(textwrap.dedent(toml))


class ManifestCase(unittest.TestCase):
    def setUp(self):
        self.tmp = tempfile.TemporaryDirectory()
        self.root = Path(self.tmp.name)
        for name, toml in CHAIN.items():
            write_test(self.root, name, toml)
        self.all = load_all(self.root)

    def tearDown(self):
        self.tmp.cleanup()

    def add(self, name, toml):
        """Add a test to the scratch tree. A manifest that fails to load is
        rolled back, so a rejection test leaves the tree loadable."""
        write_test(self.root, name, toml)
        try:
            self.all = load_all(self.root)
        except ValueError:
            shutil.rmtree(self.root / name)
            raise


class TestLoad(ManifestCase):
    def test_fields(self):
        self.assertEqual(set(self.all), set(CHAIN))
        t = self.all["adopt-node"]
        self.assertEqual((t.env, t.start, t.saves), ("node", "one-node",
                                                     "two-nodes"))
        self.assertEqual(self.all["smoke"].saves, None)
        self.assertFalse(t.mutates)
        self.assertEqual(t.steps, [])

    def test_env_is_required_and_closed(self):
        with self.assertRaisesRegex(ValueError, "declares no env"):
            self.add("no-env", 'start = "null"\nnodes = ["node1"]\n')
        with self.assertRaisesRegex(ValueError, "bad env"):
            self.add("bad-env", 'env = "lan"\nnodes = ["node1"]\n')

    def test_steps_are_netsim_only(self):
        with self.assertRaisesRegex(ValueError, "only under env netsim"):
            self.add("stepped", """
                env = "node"
                nodes = ["node1"]
                steps = ["driver"]
            """)
        self.add("vm-test", """
            env = "netsim"
            start = "two-nodes"
            nodes = ["node1", "node2"]
            steps = ["vm:enable-tor node1 node2", "driver"]
        """)
        self.assertEqual(self.all["vm-test"].steps,
                         ["vm:enable-tor node1 node2", "driver"])

    def test_typo_in_a_key_is_rejected(self):
        # why: `save` would silently make a producer a non-producer
        with self.assertRaisesRegex(ValueError, "unknown manifest key"):
            self.add("typo", """
                env = "node"
                nodes = ["node1"]
                save = "one-node"
            """)

    def test_restart_names_roster_nodes(self):
        self.assertEqual(self.all["adopt-node"].restart, [])
        with self.assertRaisesRegex(ValueError, "restart names"):
            self.add("stray-restart", """
                env = "node"
                nodes = ["node1"]
                restart = ["node2"]
            """)
        self.add("restarting", """
            env = "node"
            start = "one-node"
            nodes = ["node1"]
            restart = ["node1"]
        """)
        self.assertEqual(self.all["restarting"].restart, ["node1"])

    def test_unknown_driver_is_rejected(self):
        with self.assertRaisesRegex(ValueError, "bad drivers"):
            self.add("bad-driver", """
                env = "node"
                nodes = ["node1"]
                drivers = ["human"]
            """)


class TestPlannerKeys(ManifestCase):
    def test_absent_keys_default(self):
        t = self.all["smoke"]
        self.assertEqual((t.after, t.destroys, t.ops, t.local_only, t.facts,
                          t.prompt), (None, False, [], None, [], False))

    def test_keys_parse(self):
        self.add("vm-test", """
            env = "netsim"
            nodes = ["node1", "node2"]
            after = "adopt-node"
            ops = ["enable-tor node1 node2"]
            facts = ["tor_link_networks"]
        """)
        t = self.all["vm-test"]
        self.assertEqual(t.after, "adopt-node")
        self.assertEqual(t.ops, ["enable-tor node1 node2"])
        self.assertEqual(t.facts, ["tor_link_networks"])
        self.assertTrue(t.destroys, "ops imply destroys")

    def test_prompt_is_a_file_beside_the_manifest(self):
        (self.root / "smoke" / "prompt.md").write_text("probe the node\n")
        self.assertTrue(load_all(self.root)["smoke"].prompt)

    def test_after_must_name_a_test(self):
        with self.assertRaisesRegex(ValueError, "after names no test"):
            self.add("orphan", """
                env = "node"
                nodes = ["node1"]
                after = "adopt-nod"
            """)

    def test_an_after_loop_is_rejected(self):
        write_test(self.root, "a", 'env = "node"\nnodes = ["n"]\nafter = "b"\n')
        write_test(self.root, "b", 'env = "node"\nnodes = ["n"]\nafter = "a"\n')
        with self.assertRaisesRegex(ValueError, "loops"):
            load_all(self.root)

    def test_key_types_are_checked(self):
        for key, value, says in (("after", "1", "after must name"),
                                 ("destroys", '"yes"', "destroys must be"),
                                 ("facts", '"user_id"', "facts must be"),
                                 ("facts", "[1]", "facts must be")):
            with self.subTest(key=key, value=value):
                with self.assertRaisesRegex(ValueError, says):
                    self.add("typed", f"""
                        env = "node"
                        nodes = ["node1"]
                        {key} = {value}
                    """)

    def test_local_only_is_a_reason(self):
        with self.assertRaisesRegex(ValueError, "local_only must be"):
            self.add("bare", """
                env = "node"
                nodes = ["node1"]
                local_only = ""
            """)

    def test_ops_are_strings_and_never_local_only(self):
        with self.assertRaisesRegex(ValueError, "ops must be"):
            self.add("bad-ops", """
                env = "netsim"
                nodes = ["node1"]
                ops = [1]
            """)
        with self.assertRaisesRegex(ValueError, "cannot be local_only"):
            self.add("both", """
                env = "netsim"
                nodes = ["node1"]
                ops = ["enable-tor node1"]
                local_only = "reads a host path"
            """)


class TestConfigKey(ManifestCase):
    def test_absent_config_is_empty(self):
        self.assertEqual(self.all["smoke"].config, {})

    def test_config_tables_parse_by_node_and_module(self):
        self.add("tuned", """
            env = "node"
            nodes = ["node1"]
            [config.node1.mcp]
            bind_mcp = ""
            [config.node1.objects]
            external_registration_sweep_interval = "2s"
            tags = ["a", "b"]
        """)
        self.assertEqual(self.all["tuned"].config, {"node1": {
            "mcp": {"bind_mcp": ""},
            "objects": {"external_registration_sweep_interval": "2s",
                        "tags": ["a", "b"]}}})

    def test_bad_config_is_rejected(self):
        for body, says in (
                ('config = "x"', "must be \\[config"),
                ("[config.node2.mcp]\nbind_mcp = \"\"", "which the roster"),
                ("[config.node1]\nbind_mcp = \"\"", "must be \\[config"),
                ("[config.node1.Mcp]\nbind_mcp = \"\"", "must be \\[config"),
                ("[config.node1.mcp]", "must be \\[config"),
                ("[config.node1.mcp]\nat = 2026-10-29", "no module config"),
                ("[config.node1.objects]\nx = nan", "JSON cannot hold"),
                ("[config.node1.objects]\nx = [inf]", "JSON cannot hold"),
                ("[config.node1.apphost]\ntokens = {}", "harness's to set"),
                ("[config.node1.tcp]\nlisten_port = 1", "harness's to set"),
                ("[config.node1.mcp]\nbind_mcp = \"tcp:x\"",
                 "harness's to set")):
            with self.subTest(body=body):
                with self.assertRaisesRegex(ValueError, says):
                    self.add("tuned", 'env = "node"\nnodes = ["node1"]\n'
                             + body + "\n")


class TestResolve(ManifestCase):
    def test_full_run_order_and_kinds(self):
        # smoke builds nothing, so it runs before the producer at its depth
        plan = resolve(self.all, only=None)
        self.assertEqual([t.name for t, _ in plan],
                         ["smoke", "bootstrap-user-software-key", "adopt-node"])
        self.assertTrue(all(kind == "test" for _, kind in plan))

    def test_only_builds_prereq_prefix(self):
        plan = resolve(self.all, only=["adopt-node"])
        self.assertEqual([(t.name, k) for t, k in plan],
                         [("bootstrap-user-software-key", "prereq"),
                          ("adopt-node", "test")])

    def test_errors(self):
        with self.assertRaises(ValueError):
            resolve(self.all, only=["no-such"])
        self.add("orphan", """
            env = "node"
            start = "missing-state"
            nodes = ["node1"]
        """)
        with self.assertRaisesRegex(ValueError, "no test saves state"):
            resolve(self.all, only=["orphan"])

    def test_unrelated_broken_chain_does_not_break_selection(self):
        self.add("broken", """
            env = "node"
            start = "missing-state"
            nodes = ["node1"]
        """)
        plan = resolve(self.all, only=["smoke"])
        self.assertEqual([(t.name, k) for t, k in plan], [("smoke", "test")])

    def test_cycle_detected(self):
        self.add("cyc-a", """
            env = "node"
            start = "state-b"
            saves = "state-a"
            nodes = ["node1"]
        """)
        self.add("cyc-b", """
            env = "node"
            start = "state-a"
            saves = "state-b"
            nodes = ["node1"]
        """)
        with self.assertRaisesRegex(ValueError, "cycle"):
            resolve(self.all, only=["cyc-a"])

    def test_second_producer_of_a_state_is_rejected(self):
        self.add("import-user-software-key", """
            env = "node"
            start = "null"
            saves = "one-node"
            nodes = ["node1"]
        """)
        with self.assertRaisesRegex(ValueError, "more than one producer"):
            resolve(self.all, only=["adopt-node"])


class TestWalk(ManifestCase):
    def branch(self):
        """object-store extends two-nodes; expel-node mutates it."""
        self.add("object-store", """
            env = "node"
            start = "two-nodes"
            saves = "two-nodes-data"
            nodes = ["node1", "node2"]
        """)
        self.add("expel-node", """
            env = "node"
            start = "two-nodes"
            saves = "two-nodes-expel"
            mutates = true
            nodes = ["node1", "node2"]
        """)

    def test_main_order_is_accepted(self):
        validate_order(self.all, ["smoke", "bootstrap-user-software-key",
                                  "adopt-node"])

    def test_misordered_suite_is_rejected(self):
        # the walk seeds at adopt-node's start, so what catches this order is
        # bootstrap re-producing a one-node the walk already stands on
        with self.assertRaisesRegex(ValueError, "already built"):
            validate_order(self.all, ["adopt-node",
                                      "bootstrap-user-software-key"])

    def test_a_suite_may_open_partway_up_the_chain(self):
        # an attached daemon may already stand at two-nodes: attach checks
        # that state instead of building it
        self.branch()
        validate_order(self.all, ["object-store"])
        validate_order(self.all, ["object-store", "expel-node"])

    def test_a_producer_may_not_rebuild_a_state_the_walk_stands_on(self):
        self.branch()
        with self.assertRaisesRegex(ValueError, "already built"):
            validate_order(self.all, ["object-store", "adopt-node"])

    def test_ancestor_start_is_placeable(self):
        # expel-node starts at two-nodes while the walk stands at
        # two-nodes-data — an ancestor start is legal, equality is not required
        self.branch()
        validate_order(self.all, ["bootstrap-user-software-key", "adopt-node",
                                  "object-store", "expel-node"])

    def test_mutator_fences_its_own_branch(self):
        self.branch()
        with self.assertRaisesRegex(ValueError, "not on the "
                                    "'two-nodes-expel' branch"):
            validate_order(self.all, ["bootstrap-user-software-key",
                                      "adopt-node", "expel-node",
                                      "object-store"])

    def test_nothing_follows_a_mutator_that_saves_nothing(self):
        self.add("wreck", """
            env = "node"
            start = "one-node"
            mutates = true
            nodes = ["node1"]
        """)
        with self.assertRaisesRegex(ValueError, "nothing may follow"):
            validate_order(self.all, ["bootstrap-user-software-key", "wreck",
                                      "adopt-node"])

    def test_unknown_test_in_a_suite_is_rejected(self):
        with self.assertRaisesRegex(ValueError, "unknown test"):
            validate_order(self.all, ["smoke", "no-such-test"])


if __name__ == "__main__":
    unittest.main()
