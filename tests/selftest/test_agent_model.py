"""The operator-model rewrite, checked without a VM.

why: the rewrite is Python inside shell inside one ssh argv, and a quoting
slip there fails minutes into a VM run with a shell error nobody can read.
Rendering it here costs nothing and catches exactly that.
"""
import unittest

from lib.executors.netsim import NetsimExecutor


def executor(model="muse-glimmer-30b-p6", base_url=""):
    ex = NetsimExecutor.__new__(NetsimExecutor)
    ex._agent_model, ex._agent_base_url, ex._model_applied = model, base_url, False
    return ex


class TestAgentModel(unittest.TestCase):
    def render(self, **kw):
        ex, box = executor(**kw), {}

        def ssh(vm, script):
            box["script"] = script
            return f"OPENAI_MODEL={ex._agent_model}"

        ex._ssh = ssh
        ex._apply_model()
        return ex, box.get("script", "")

    def test_embedded_python_is_valid(self):
        _, script = self.render()
        body = script.split("<<'PY'\n", 1)[1].split("\nPY", 1)[0]
        compile(body, "<embedded>", "exec")     # raises on a quoting slip

    def test_names_the_model_and_verifies_it_took(self):
        ex, script = self.render()
        self.assertIn("muse-glimmer-30b-p6", script)
        self.assertIn("grep OPENAI_MODEL=", script)
        self.assertTrue(ex._model_applied)

    def test_applies_once(self):
        ex, _ = self.render()
        calls = []
        ex._ssh = lambda vm, s: calls.append(s) or "OPENAI_MODEL=x"
        ex._apply_model()
        self.assertEqual(calls, [], "a second apply must be a no-op")

    def test_no_model_is_a_no_op(self):
        ex = executor(model="")
        ex._ssh = lambda vm, s: self.fail("must not ssh when no model is set")
        ex._apply_model()

    def test_a_rewrite_that_did_not_take_is_an_error(self):
        ex = executor()
        ex._ssh = lambda vm, s: "OPENAI_MODEL=the-old-one"
        with self.assertRaises(Exception):
            ex._apply_model()


if __name__ == "__main__":
    unittest.main()
