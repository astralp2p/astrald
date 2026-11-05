"""The agent driver, against a guest that only records what it was sent.

why: the operator is driven through shell inside one ssh argv, and a
quoting slip or a wrong file there fails minutes into a VM run. Rendered
here, it costs nothing.
"""
import json
import subprocess
import tempfile
import unittest
from pathlib import Path
from types import SimpleNamespace

from lib import agent
from lib.worlds import NODE_FIELDS, WorldError


class Broken:
    """An answer whose ssh transport fails: the world's ssh raises when
    checked and hands back empty output when not, as _netsim does."""


class Guest:
    """An ssh(vm, script) that answers by the first command it recognises."""

    def __init__(self, answers=None):
        self.scripts, self.answers = [], dict(answers or {})

    def __call__(self, vm, script, check=True, timeout=None):
        self.scripts.append(script)
        for needle, answer in self.answers.items():
            if needle in script:
                if answer is Broken:
                    if check:
                        raise WorldError("netsim ssh: exit 255")
                    return ""
                if isinstance(answer, BaseException):
                    raise answer
                return answer(script) if callable(answer) else answer
        return ""


def session(transcript: str, rc: int):
    """A guest answer to the operator's session: the transcript `cat` prints,
    then each echo after it, as the guest's shell runs them; stripped, as
    the world's ssh strips its output."""
    def answer(script: str) -> str:
        out = transcript
        for line in script.split("\n")[script.split("\n").index(
                next(l for l in script.split("\n") if l.startswith("cat "))) + 1:]:
            if line == "echo":
                out += "\n"
            elif line.startswith('echo "'):
                out += line[6:-1].replace("$rc", str(rc)) + "\n"
        return out.strip()
    return answer


def a_test(root: Path, name: str, prompt: str, facts=(), files=()) -> object:
    d = root / name
    d.mkdir(parents=True)
    (d / "prompt.md").write_text(prompt)
    for f, text in files:
        (d / f).write_text(text)
    return SimpleNamespace(name=name, dir=d, facts=list(facts))


def an_art(root: Path, view: dict) -> Path:
    art = root / "art"
    art.mkdir()
    (art / "session.json").write_text(json.dumps(view))
    return art


VIEW = {"run": "s", "facts": {"peer_object_id": "obj-1"},
        "nodes": {"node2": {"identity": "02ab", "endpoint": "tcp:x"}}}


class TestRender(unittest.TestCase):
    def setUp(self):
        self.tmp = tempfile.TemporaryDirectory()
        self.root = Path(self.tmp.name)

    def tearDown(self):
        self.tmp.cleanup()

    def test_placeholders_fill_from_the_view(self):
        t = a_test(self.root, "t", "read {facts.peer_object_id} from "
                   "{node2.alias} ({node2.identity}); {not a placeholder}\n")
        self.assertEqual(agent.render(t, VIEW),
                         "read obj-1 from node2 (02ab); {not a placeholder}\n")

    def test_the_lineage_files_are_named_before_the_footer(self):
        t = a_test(self.root, "t", "do it\n", facts=["x"])
        text = agent.render(t, VIEW, kept=["boot", "peer"])
        self.assertEqual(text.split("\n\n")[1],
                         "Earlier steps of this flow left what they learned "
                         "in `~/.facts/boot.json`, `~/.facts/peer.json`.")
        self.assertTrue(text.endswith(agent.footer(t)))
        self.assertEqual(agent.render(t, VIEW, kept=()),
                         agent.render(t, VIEW))

    def test_an_unknown_placeholder_is_refused(self):
        for text in ("{facts.object_id}", "{node9.identity}",
                     "{node2.mcp_url}", "{node2.endpoint}"):
            with self.subTest(text=text):
                t = SimpleNamespace(name="t", dir=self.root, facts=[])
                (self.root / "prompt.md").write_text(text)
                with self.assertRaises(agent.PromptError):
                    agent.render(t, VIEW)

    def test_no_placeholder_hands_the_operator_a_token(self):
        # why: the operator is judged with the node tokens and must never
        # hold one, so neither a node's token nor a token fact renders
        view = dict(VIEW, facts={"user_token": "secret"},
                    nodes={"node2": {"identity": "02ab", "token": "secret"}})
        for text in ("{node2.token}", "{facts.user_token}"):
            with self.subTest(text=text):
                t = SimpleNamespace(name="t", dir=self.root, facts=[])
                (self.root / "prompt.md").write_text(text)
                with self.assertRaises(agent.PromptError):
                    agent.render(t, view)

    def test_a_node_renders_only_its_identity_alias_and_link_endpoint(self):
        # why a view that holds every node field: a field the view lacks is
        # refused whatever the allow-list says, so only a full view shows the
        # allow-list deciding. lan_endpoint is the address a peer links to and
        # grants nothing; endpoint, token and root are the harness's way in
        view = dict(VIEW, nodes={"node2": {f: f"v-{f}" for f in NODE_FIELDS}})
        t = a_test(self.root, "t", "{node2.alias} {node2.identity} "
                   "{node2.lan_endpoint}\n")
        self.assertEqual(agent.render(t, view),
                         "node2 v-identity v-lan_endpoint\n")
        for field in sorted(set(NODE_FIELDS) - {"identity", "lan_endpoint"}):
            with self.subTest(field=field):
                (t.dir / "prompt.md").write_text(f"{{node2.{field}}}")
                with self.assertRaisesRegex(agent.PromptError,
                                            "which no prompt may carry"):
                    agent.render(t, view)

    def test_the_footer_names_exactly_the_declared_facts(self):
        t = a_test(self.root, "t", "do it\n", facts=["a_id", "b_token"])
        text = agent.render(t, VIEW)
        self.assertTrue(text.startswith("do it\n"))
        self.assertIn("`~/.facts/t.json`", text)
        self.assertIn("exactly `a_id`, `b_token`, each holding", text)
        one = a_test(self.root, "u", "do it\n", facts=["a_id"])
        self.assertIn("exactly `a_id`, holding", agent.footer(one))

    def test_a_test_that_declares_no_facts_gets_no_footer(self):
        t = a_test(self.root, "t", "do it\n")
        self.assertEqual(agent.render(t, VIEW), "do it\n")


class TestOperator(unittest.TestCase):
    def setUp(self):
        self.tmp = tempfile.TemporaryDirectory()
        self.root = Path(self.tmp.name)

    def tearDown(self):
        self.tmp.cleanup()

    def run_op(self, test, guest, model=""):
        art = an_art(self.root, VIEW)
        rc = agent.Operator(guest, model=model).run(test, art)
        return rc, art

    def test_facts_come_from_the_tests_own_file_alone(self):
        # why: facts were a merge of every ~/*.json in the operator's home,
        # so a stale ~/object.json answered for object-store's oracle
        t = a_test(self.root, "object-store", "store it\n",
                   facts=["object_id"], files=[("payload.txt", "hi")])
        guest = Guest({"su - tester": f"done\n{agent.EXIT}0",
                       "cat /home/tester/.facts/object-store.json":
                       '{"object_id": "fresh", "extra": 1}',
                       "cat /home/tester/object.json": '{"object_id": "stale"}'})
        rc, art = self.run_op(t, guest)
        self.assertEqual(rc, 0)
        self.assertEqual(json.loads((art / "facts.json").read_text()),
                         {"object_id": "fresh"})
        push = guest.scripts[0]
        self.assertIn("rm -f /home/tester/.facts/object-store.json", push)
        self.assertIn("> /home/tester/payload.txt", push)
        self.assertNotIn("prompt.md", push)
        self.assertFalse(any("/home/tester/object.json" in s
                             for s in guest.scripts))

    def test_a_session_removes_only_its_own_facts_file(self):
        # why: bootstrap's operator keeps the User token in its own file,
        # and emptying ~/.facts/ took it from every later session
        t = a_test(self.root, "adopt-node", "adopt it\n")
        guest = Guest({"su - tester": f"done\n{agent.EXIT}0"})
        rc, _ = self.run_op(t, guest)
        self.assertEqual(rc, 0)
        push = guest.scripts[0].split("\n")
        self.assertEqual([line for line in push if line.startswith("rm ")],
                         ["rm -f /home/tester/.facts/adopt-node.json"])
        self.assertIn("install -d -m 700 -o tester -g tester "
                      "/home/tester/.facts", push)

    def test_a_missing_declared_fact_is_a_driver_failure(self):
        t = a_test(self.root, "t", "do it\n", facts=["a_id", "b_id"])
        guest = Guest({"su - tester": f"done\n{agent.EXIT}0",
                       "cat /home/tester/.facts/t.json": '{"a_id": 1}'})
        rc, art = self.run_op(t, guest)
        self.assertNotEqual(rc, 0)
        self.assertFalse((art / "facts.json").exists())
        self.assertIn("lacks the declared fact(s) b_id",
                      (art / "driver.log").read_text())

    def test_the_operators_exit_code_is_the_drivers(self):
        t = a_test(self.root, "t", "do it\n")
        rc, art = self.run_op(t, Guest({"su - tester": f"gave up\n{agent.EXIT}3"}))
        self.assertEqual(rc, 3)
        self.assertIn("gave up", (art / "driver.log").read_text())

    def test_an_unknown_placeholder_is_a_driver_failure_on_no_guest(self):
        # why driver: the prompt is the test's defect, and a WorldError
        # recorded it as the world breaking
        t = a_test(self.root, "t", "read {facts.nope}\n")
        guest = Guest()
        rc, art = self.run_op(t, guest, model="m")
        self.assertEqual(rc, 1)
        self.assertEqual(guest.scripts, [])
        self.assertIn("[agent] t: prompt.md names {facts.nope}",
                      (art / "driver.log").read_text())

    def test_a_transport_that_breaks_while_collecting_is_the_worlds(self):
        # why: an ssh exit 255 read as an operator that left every fact out
        t = a_test(self.root, "t", "do it\n", facts=["a_id"])
        guest = Guest({"su - tester": f"done\n{agent.EXIT}0",
                       "cat /home/tester/.facts/t.json": Broken})
        art = an_art(self.root, VIEW)
        with self.assertRaisesRegex(WorldError, "exit 255"):
            agent.Operator(guest).run(t, art)
        self.assertFalse((art / "facts.json").exists())
        self.assertNotIn("lacks", (art / "driver.log").read_text())

    def test_a_transcript_without_a_final_newline_keeps_its_exit_code(self):
        t = a_test(self.root, "t", "do it\n")
        rc, _ = self.run_op(t, Guest({"su - tester": session("all done.", 0)}))
        self.assertEqual(rc, 0)
        rc, _ = self.run_op_again(t, Guest({"su - tester":
                                            session("gave up.", 3)}))
        self.assertEqual(rc, 3)

    def run_op_again(self, test, guest):
        art = self.root / "art"
        return agent.Operator(guest).run(test, art), art

    def test_a_broken_session_keeps_the_transcript_and_raises(self):
        # why: netsim ssh exiting 255 mid-session is the world breaking, and
        # the transcript the guest still holds says how far the operator got
        t = a_test(self.root, "t", "do it\n")
        guest = Guest({"su - tester": WorldError("netsim ssh: exit 255"),
                       "tail -c 4000": "halfway there"})
        art = an_art(self.root, VIEW)
        with self.assertRaises(WorldError):
            agent.Operator(guest).run(t, art)
        log = (art / "driver.log").read_text()
        self.assertIn("exit 255", log)
        self.assertIn("halfway there", log)

    def test_a_timeout_keeps_the_transcript_tail(self):
        t = a_test(self.root, "t", "do it\n")
        guest = Guest({"su - tester": subprocess.TimeoutExpired("ssh", 1),
                       "tail -c 4000": "still thinking"})
        rc, art = self.run_op(t, guest)
        self.assertEqual(rc, 124)
        self.assertIn("still thinking", (art / "driver.log").read_text())

    def test_a_timed_out_operator_is_killed_on_its_vm(self):
        # why: the timeout ends only the host's ssh, and the operator went
        # on acting on node1 while the next test shared its world
        t = a_test(self.root, "t", "do it\n")
        guest = Guest({"su - tester": subprocess.TimeoutExpired("ssh", 1)})
        rc, art = self.run_op(t, guest)
        self.assertEqual(rc, 124)
        after = guest.scripts[guest.scripts.index(next(
            s for s in guest.scripts if "su - tester" in s)) + 1:]
        self.assertEqual(after[0], "pkill -KILL -u tester || true")
        self.assertIn("[agent] killed every tester process on node1",
                      (art / "driver.log").read_text())


class TestModel(unittest.TestCase):
    def rewrite(self, model="muse-glimmer-30b-p6", took=True):
        guest = Guest({"OPENAI_MODEL": f"OPENAI_MODEL={model if took else 'old'}"})
        op = agent.Operator(guest, model=model)
        return op, guest

    def test_embedded_python_is_valid(self):
        op, guest = self.rewrite()
        op._set_model()
        body = guest.scripts[0].split("<<'PY'\n", 1)[1].split("\nPY", 1)[0]
        compile(body, "<embedded>", "exec")     # raises on a quoting slip

    def test_set_once_per_world(self):
        op, guest = self.rewrite()
        op._set_model()
        op._set_model()
        self.assertEqual(len(guest.scripts), 1)

    def test_no_model_keeps_the_labs(self):
        guest = Guest()
        agent.Operator(guest)._set_model()
        self.assertEqual(guest.scripts, [])

    def test_a_rewrite_that_did_not_take_is_an_error(self):
        op, _ = self.rewrite(took=False)
        with self.assertRaises(WorldError):
            op._set_model()


if __name__ == "__main__":
    unittest.main()
