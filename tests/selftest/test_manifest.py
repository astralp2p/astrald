import shutil
import tempfile
import textwrap
import unittest
from pathlib import Path

from lib.manifest import load_all

CHAIN = {
    "smoke": """
        nodes = ["node1"]
        timeout = 60
    """,
    "bootstrap-user-software-key": """
        nodes = ["node1"]
        timeout = 120
    """,
    "adopt-node": """
        nodes = ["node1", "node2"]
        after = "bootstrap-user-software-key"
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
        self.assertEqual((t.nodes, t.after, t.timeout),
                         (["node1", "node2"], "bootstrap-user-software-key",
                          180))
        self.assertEqual(self.all["smoke"].after, None)

    def test_a_roster_is_required(self):
        with self.assertRaisesRegex(ValueError, "names no nodes"):
            self.add("rosterless", "timeout = 60\n")

    def test_typo_in_a_key_is_rejected(self):
        # why: `aftre` would silently make a continuing test a fresh one
        with self.assertRaisesRegex(ValueError, "unknown manifest key"):
            self.add("typo", """
                nodes = ["node1"]
                aftre = "smoke"
            """)

    def test_a_retired_key_is_rejected(self):
        # why: the state chain is gone, and a manifest still naming a state
        # would read as though it continued one
        for key, value in (("start", '"one-node"'), ("env", '"node"'),
                           ("mutates", "true"), ("drivers", '["agent"]')):
            with self.subTest(key=key):
                with self.assertRaisesRegex(ValueError, "unknown manifest key"):
                    self.add("retired", f"""
                        nodes = ["node1"]
                        {key} = {value}
                    """)

    def test_restart_names_roster_nodes(self):
        self.assertEqual(self.all["adopt-node"].restart, [])
        with self.assertRaisesRegex(ValueError, "restart names"):
            self.add("stray-restart", """
                nodes = ["node1"]
                restart = ["node2"]
            """)
        self.add("restarting", """
            nodes = ["node1"]
            after = "bootstrap-user-software-key"
            restart = ["node1"]
        """)
        self.assertEqual(self.all["restarting"].restart, ["node1"])

    def test_a_while_down_script_needs_a_restart(self):
        # why: nothing but a restart runs it, so a test that ships one and
        # restarts nothing would pass without its changes ever made
        self.assertFalse(self.all["smoke"].while_down)
        d = self.root / "idle"
        d.mkdir()
        (d / "while_down.py").write_text("")
        with self.assertRaisesRegex(ValueError, "^idle: ships a while_down"):
            self.add("idle", 'nodes = ["node1"]\n')
        d.mkdir()
        (d / "while_down.py").write_text("")
        self.add("idle", 'nodes = ["node1"]\nrestart = ["node1"]\n')
        self.assertTrue(self.all["idle"].while_down)

    def test_stop_names_roster_nodes_and_implies_destroys(self):
        self.assertEqual(self.all["adopt-node"].stop, [])
        with self.assertRaisesRegex(ValueError, "stop names"):
            self.add("stray-stop", """
                nodes = ["node1"]
                stop = ["node2"]
            """)
        with self.assertRaisesRegex(ValueError, "both restarted and stopped"):
            self.add("restart-and-stop", """
                nodes = ["node1"]
                restart = ["node1"]
                stop = ["node1"]
            """)
        self.add("stopping", """
            nodes = ["node1", "node2"]
            stop = ["node1"]
        """)
        t = self.all["stopping"]
        self.assertEqual((t.stop, t.destroys), (["node1"], True))


class TestPlannerKeys(ManifestCase):
    def test_absent_keys_default(self):
        t = self.all["smoke"]
        self.assertEqual((t.after, t.destroys, t.ops, t.local_only,
                          t.host_listener, t.facts, t.prompt),
                         (None, False, [], None, None, [], False))

    def test_keys_parse(self):
        self.add("vm-test", """
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
                nodes = ["node1"]
                after = "adopt-nod"
            """)

    def test_an_after_loop_is_rejected(self):
        write_test(self.root, "a", 'nodes = ["n"]\nafter = "b"\n')
        write_test(self.root, "b", 'nodes = ["n"]\nafter = "a"\n')
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
                        nodes = ["node1"]
                        {key} = {value}
                    """)

    def test_local_only_is_a_reason(self):
        with self.assertRaisesRegex(ValueError, "local_only must be"):
            self.add("bare", """
                nodes = ["node1"]
                local_only = ""
            """)

    def test_ops_are_strings_and_never_local_only(self):
        with self.assertRaisesRegex(ValueError, "ops must be"):
            self.add("bad-ops", """
                nodes = ["node1"]
                ops = [1]
            """)
        with self.assertRaisesRegex(ValueError, "cannot be local_only"):
            self.add("both", """
                nodes = ["node1"]
                ops = ["enable-tor node1"]
                local_only = "reads a host path"
            """)


    def test_host_listener_is_a_reason_beside_local_only_never_ops(self):
        # why beside local_only: a test may serve a host listener and still
        # be local only for a reason no world operation serves
        self.add("serves", """
            nodes = ["node1"]
            host_listener = " serves an app node1 dials "
            local_only = "reads a host path"
        """)
        t = self.all["serves"]
        self.assertEqual((t.host_listener, t.local_only),
                         ("serves an app node1 dials", "reads a host path"))
        with self.assertRaisesRegex(ValueError, "host_listener must be"):
            self.add("bare", """
                nodes = ["node1"]
                host_listener = ""
            """)
        with self.assertRaisesRegex(ValueError, "cannot be host_listener"):
            self.add("both", """
                nodes = ["node1"]
                ops = ["enable-tor node1"]
                host_listener = "serves an app node1 dials"
            """)


class TestConfigKey(ManifestCase):
    def test_absent_config_is_empty(self):
        self.assertEqual(self.all["smoke"].config, {})

    def test_config_tables_parse_by_node_and_module(self):
        self.add("tuned", """
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
                ("[config.node1.apphost]\nbind_http = \"tcp:127.0.0.1:8624\"",
                 "harness's to set"),
                ("[config.node1.tcp]\nlisten_port = 1", "harness's to set"),
                ("[config.node1.mcp]\nbind_mcp = \"tcp:x\"",
                 "harness's to set")):
            with self.subTest(body=body):
                with self.assertRaisesRegex(ValueError, says):
                    self.add("tuned", 'nodes = ["node1"]\n'
                             + body + "\n")


if __name__ == "__main__":
    unittest.main()
