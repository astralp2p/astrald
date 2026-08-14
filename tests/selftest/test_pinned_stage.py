"""What `--target stage:<name>` accepts, checked without a VM.

why: the obvious thing to type is the state a manifest names — `two-nodes` —
and that is not a netsim stage. Before this resolved, the run died on netsim's
own "no such stage: two-nodes" after booting nothing, and the real name
(`e2e-two-nodes-r<recipe>-g<astrald>`) is not something anyone can spell.
"""
import unittest
from pathlib import Path

from lib.executors.netsim import ExecutorError, NetsimExecutor, stage_key

RECIPE = (Path(__file__).resolve().parents[1]
          / "netsim/labs/two-node-plus-external/lab.story")
REF = "c0ffee00"


def executor(pinned, stages):
    ex = NetsimExecutor.__new__(NetsimExecutor)
    ex.recipe, ex.astrald_ref, ex.pinned = RECIPE, REF, pinned
    ex._stages = lambda: set(stages)
    return ex


class TestPinnedStage(unittest.TestCase):
    def test_state_name_resolves_to_this_recipe_and_astrald(self):
        key = stage_key("two-nodes", RECIPE, REF)
        self.assertEqual(executor("two-nodes", {key})._resolve_pinned(), key)

    def test_a_real_stage_name_is_used_as_given(self):
        """A name netsim knows wins, so naming a stage directly still works."""
        ex = executor("hand-built", {"hand-built"})
        self.assertEqual(ex._resolve_pinned(), "hand-built")

    def test_a_state_of_another_astrald_is_not_borrowed(self):
        """The whole point of the key: same state, wrong binary, no verdict."""
        stale = stage_key("two-nodes", RECIPE, "deadbeef")
        with self.assertRaises(ExecutorError):
            executor("two-nodes", {stale})._resolve_pinned()

    def test_unknown_name_names_both_things_it_looked_for(self):
        with self.assertRaises(ExecutorError) as caught:
            executor("nope", set())._resolve_pinned()
        message = str(caught.exception)
        self.assertIn("nope", message)
        self.assertIn(stage_key("nope", RECIPE, REF), message)


if __name__ == "__main__":
    unittest.main()
