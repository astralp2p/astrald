"""The agent contract: a prompt must ask for what the oracle will read.

why this exists: `import-user-software-key` ran an operator for 2404 s, the
operator performed the flow correctly and derived exactly the expected User —
and the run failed `verify` with `KeyError: 'import_user_id'`, because the
prompt told it to save `user_id` while the oracle reads `import_user_id`. The
scripted driver hid the mismatch by writing the right names itself.

A prompt is the agent driver's half of the same contract `write_facts` is for
the scripted one, so the two must name the same facts. That is a static
property of three files, and checking it here costs milliseconds instead of a
forty-minute VM run per test.
"""
import re
import unittest
from pathlib import Path

TESTS = Path(__file__).resolve().parent.parent
E2E = TESTS / "e2e"

WRITE_FACTS = re.compile(r"write_facts\(\s*\{(.*?)\}", re.S)
KEY = re.compile(r"[\"']([A-Za-z_][A-Za-z0-9_]*)[\"']\s*:")
READ_FACT = re.compile(r"""facts["']\]\[["']([A-Za-z_][A-Za-z0-9_]*)""")


def facts_written(test_dir: Path) -> set:
    script = test_dir / "script.py"
    if not script.exists():
        return set()
    out = set()
    for block in WRITE_FACTS.findall(script.read_text()):
        out |= set(KEY.findall(block))
    return out


def facts_read(test_dir: Path) -> set:
    verify = test_dir / "verify.py"
    if not verify.exists():
        return set()
    return set(READ_FACT.findall(verify.read_text()))


class TestPrompts(unittest.TestCase):
    def agent_tests(self):
        for d in sorted(E2E.iterdir()):
            if (d / "prompt.md").exists():
                yield d

    def test_every_agent_test_ships_a_prompt(self):
        # the mirror of the rule below: declaring the driver without a prompt
        # is a run that dies in the executor rather than in a verdict.
        for d in sorted(E2E.iterdir()):
            manifest = (d / "test.toml").read_text()
            if '"agent"' in manifest:
                self.assertTrue((d / "prompt.md").exists(),
                                f"{d.name} declares the agent driver, no prompt.md")

    def test_prompt_names_the_facts_its_oracle_reads(self):
        for d in self.agent_tests():
            # why the intersection: an oracle also reads facts produced by
            # tests EARLIER in the chain, and this test's operator is not
            # responsible for those. What it owes is exactly what its own
            # scripted driver would have written and its own oracle reads.
            owed = facts_written(d) & facts_read(d)
            if not owed:
                continue
            prompt = (d / "prompt.md").read_text()
            for key in sorted(owed):
                self.assertIn(
                    key, prompt,
                    f"{d.name}: the oracle reads facts[{key!r}] and script.py "
                    f"writes it, but prompt.md never names it — an operator "
                    f"would perform the flow and still fail verify")


if __name__ == "__main__":
    unittest.main()
