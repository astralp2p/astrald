"""The operator-model rewrite, checked without a VM.

why: the rewrite is Python inside shell inside one ssh argv, and a quoting
slip there fails minutes into a VM run with a shell error nobody can read.
Rendering it here costs nothing and catches exactly that.
"""
import unittest

from lib.executors.netsim import DEFAULT_OPERATOR, NetsimExecutor


def executor(model="muse-glimmer-30b-p6", base_url=""):
    ex = NetsimExecutor.__new__(NetsimExecutor)
    ex._agent_model, ex._agent_base_url, ex._model_applied = model, base_url, False
    ex.operator = dict(DEFAULT_OPERATOR)
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


class TestOperatorProfile(unittest.TestCase):
    """The profile is what makes a second agent a config entry, not a patch."""

    def test_a_profile_overrides_where_and_how(self):
        ex = executor()
        ex.use_operator({"vm": "node2", "user": "pilot",
                         "command": 'claude -p "$(cat {prompt})"'})
        self.assertEqual(ex.operator["vm"], "node2")
        self.assertEqual(ex.operator["user"], "pilot")
        self.assertIn("claude -p", ex.operator["command"])

    def test_a_partial_profile_keeps_the_defaults(self):
        # why: a profile that only names its command should not have to
        # restate where the lab puts its operator.
        ex = executor()
        ex.use_operator({"command": "aider --message-file {prompt}"})
        self.assertEqual(ex.operator["vm"], DEFAULT_OPERATOR["vm"])
        self.assertEqual(ex.operator["user"], DEFAULT_OPERATOR["user"])

    def test_the_prompt_path_is_substituted(self):
        ex = executor()
        ex.use_operator({"command": "aider --message-file {prompt}"})
        rendered = ex.operator["command"].format(prompt="/home/x/.netsim/t.prompt")
        self.assertEqual(rendered, "aider --message-file /home/x/.netsim/t.prompt")
        self.assertNotIn("{prompt}", rendered)
