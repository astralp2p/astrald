"""The shipped prompts, rendered the way the agent driver renders them.

why this exists: `import-user-software-key` ran an operator for 2404 s, the
operator performed the flow correctly and derived exactly the expected User —
and the run failed `verify` with `KeyError: 'import_user_id'`, because the
prompt told it to save `user_id` while the oracle reads `import_user_id`.
The footer now names the facts from the manifest, and test_shipped holds
every fact an oracle reads to what the manifests declare, so a prompt can no
longer ask for the wrong name. What is left to check is that every prompt
renders: against a view of its roster and its lineage's facts, nothing is
left unfilled, and the footer asks for exactly the declared facts.
"""
import re
import unittest
from pathlib import Path

from lib import agent
from lib.manifest import load_all
from lib.plan import chain

TESTS = Path(__file__).resolve().parent.parent
# a node's link endpoint in each kind of world: loopback beside a local
# world's other nodes, the simulated LAN in a VM world (lib/worlds/local.py,
# lib/worlds/vm.py)
SHAPES = {"local": lambda i: f"tcp:127.0.0.1:{20800 + i}",
          "vm": lambda i: f"tcp:10.0.0.{i + 1}:1791"}
LEFTOVER = re.compile(r"\{[a-z][a-z0-9_-]*\.[A-Za-z_][A-Za-z0-9_]*\}|__[A-Z][A-Z0-9_]*__")


class TestPrompts(unittest.TestCase):
    @classmethod
    def setUpClass(cls):
        cls.all = load_all(TESTS / "e2e")
        cls.agent_tests = [t for _, t in sorted(cls.all.items()) if t.prompt]

    def view(self, t, world="local") -> dict:
        """What a step of t would see: its roster, and a value for every
        fact its lineage declares."""
        lineage = chain(self.all, t.name)[:-1]
        return {"run": "stamp",
                "nodes": {n: {"identity": f"id-{n}", "endpoint": "tcp:x",
                              "lan_endpoint": SHAPES[world](i)}
                          for i, n in enumerate(t.nodes)},
                "facts": {f: f"<{f}>" for n in lineage
                          for f in self.all[n].facts}}

    def test_every_operator_test_ships_a_prompt(self):
        self.assertEqual(len(self.agent_tests), 12)

    def test_every_prompt_renders_with_nothing_left_unfilled(self):
        for t in self.agent_tests:
            for world in SHAPES:
                with self.subTest(test=t.name, world=world):
                    text = agent.render(t, self.view(t, world))
                    self.assertEqual(LEFTOVER.findall(text), [])

    def test_every_footer_asks_for_exactly_the_declared_facts(self):
        for t in self.agent_tests:
            with self.subTest(test=t.name):
                foot = agent.footer(t)
                self.assertEqual(re.findall(r"`([a-z_]+)`", foot), t.facts)
                self.assertEqual(bool(foot), bool(t.facts))

    def test_no_prompt_names_a_file_of_its_own_for_facts(self):
        # why: the footer is the one place an operator learns where its facts
        # go, and a prompt that still says "save it to ~/x.json" would split
        # them across two files, only one of which is ever read
        for t in self.agent_tests:
            with self.subTest(test=t.name):
                text = (t.dir / "prompt.md").read_text()
                self.assertNotRegex(text, r"~/[\w.-]+\.json")

    def test_no_prompt_assumes_a_vm_world(self):
        # why: a prompt is judged in a local world too, where no node has a
        # hostname of its own, nothing is discovered on a LAN, and the
        # operator does not sit on the node it works
        vm_only = re.compile(r"\bhostname\b|\blocal network\b|\bthis (node|network)\b",
                             re.I)
        for t in self.agent_tests:
            with self.subTest(test=t.name):
                text = (t.dir / "prompt.md").read_text()
                self.assertNotRegex(text, vm_only)

    def test_every_prompt_names_its_nodes_by_placeholder(self):
        # why: a roster name typed into a prompt means something only where a
        # script set it as an alias, and a node's own alias is random
        for t in self.agent_tests:
            with self.subTest(test=t.name):
                text = agent.PLACEHOLDER.sub(
                    "", (t.dir / "prompt.md").read_text())
                for n in t.nodes:
                    self.assertNotRegex(text, rf"\b{re.escape(n)}\b")


if __name__ == "__main__":
    unittest.main()
