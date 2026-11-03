"""The agent driver: the lab's AI operator, driven over a VM world's ssh.

The lab bakes one operator: Qwen Code on node1, run as `tester`, with the
astral-agent skill (netsim/labs/two-node-plus-external/lab.story). It reaches
astrald the way an app would, over the guest's own apphost, and produces no
Python: it reads the test's prompt and leaves its facts as JSON in its home,
where the same verify.py that judges the scripted driver judges them.

A step's prompt is the test's prompt.md, its placeholders filled from the
step's session view, and a footer naming the file and the facts it owes:

    {facts.<name>}       a fact the test's lineage learned, never a token
    {<node>.identity}    a roster node's identity
    {<node>.alias}       its roster name, which is also its hostname in the lab

The footer asks for every declared fact, including ones no oracle reads —
expel-node's `expelled_id`, tor-link's `tor_link_networks`. That is
deliberate: `facts` is one contract, what the test's driver hands on,
whoever drives it.

The operator, `tester`, never holds a node token or the world record: they
are what it is judged with. So nothing token-bearing reaches its home, its
prompt or its payloads.
"""
import json
import re
import shlex
import subprocess
from base64 import b64encode
from pathlib import Path

from lib.worlds import WorldError

NAME = "qwen"                    # the operator the results header names
VM = "node1"                     # where the lab bakes it
USER = "tester"                  # who it runs as
HOME = f"/home/{USER}"
COMMAND = 'qwen -y "$(cat {prompt})"'
# why this large: an operator reads a prompt, plans, and works the node
# through its own shell, and one measured import-user-software-key session
# took 2404 s — past the 2400 s budget the manifests used to carry
TIMEOUT = 3600
# the test's files the operator never sees: it is not supposed to read the test
HARNESS_FILES = {"test.toml", "script.py", "verify.py", "prompt.md",
                 "README.md"}
PLACEHOLDER = re.compile(r"\{([a-z][a-z0-9_-]*)\.([A-Za-z_][A-Za-z0-9_]*)\}")
# the node fields a prompt may name: a node's endpoint and token are the
# harness's way in, never the operator's
NODE_PLACEHOLDERS = {"identity", "alias"}
EXIT = "[agent] exit "           # the line the guest ends a session with
EXIT_LINE = re.compile(r"^\[agent\] exit (\d+)$", re.M)


def render(test, view: dict) -> str:
    """The prompt the operator gets for test: prompt.md filled from the
    step's view, then the facts footer.

    why an unknown placeholder raises before anything runs: a prompt that
    names a fact or node the world does not hold asks the operator for
    something nobody can do, and that is the test's defect, not the
    operator's.
    """
    def fill(m: re.Match) -> str:
        root, field = m.groups()
        if root == "facts" and "token" not in field:
            source = view.get("facts", {})
        elif root in view.get("nodes", {}) and field in NODE_PLACEHOLDERS:
            source = dict(view["nodes"][root], alias=root)
        else:
            raise WorldError(f"{test.name}: prompt.md names {m.group(0)}, "
                             "which no prompt may carry")
        if source.get(field) in (None, ""):
            raise WorldError(f"{test.name}: prompt.md names {m.group(0)}, "
                             "which this step's view does not hold")
        return str(source[field])

    text = (test.dir / "prompt.md").read_text()
    return PLACEHOLDER.sub(fill, text).rstrip("\n") + "\n" + footer(test)


def footer(test) -> str:
    """Where the operator leaves its facts, and exactly which: the test's
    declared `facts`. A test that declares none owes none."""
    if not test.facts:
        return ""
    keys = ", ".join(f"`{f}`" for f in test.facts)
    holds = ("holding the value its name describes" if len(test.facts) == 1
             else "each holding the value its name describes")
    return (f"\nWhen you are done, write `~/.facts/{test.name}.json`: one "
            f"JSON object whose keys are exactly {keys}, {holds}.\n")


class Operator:
    """The lab's operator in one VM world.

    why one per world: the model is set once per world, and a world is
    what boots with the lab's own model baked in.
    """

    def __init__(self, ssh, model: str = "", base_url: str = ""):
        self.ssh = ssh                # the VM world's ssh(vm, script, …)
        self.model, self.base_url = model, base_url
        self._model_set = not model

    def run(self, test, art: Path, timeout: int = TIMEOUT) -> int:
        """Drive test's flow; the driver's exit code.

        The step's view is art/session.json and its facts go to
        art/facts.json, as a scripted driver's do. A declared fact missing
        from the operator's file is a non-zero exit — a `driver` failure:
        the flow it owed never fully happened.
        """
        prompt = render(test, json.loads((art / "session.json").read_text()))
        self._set_model()
        self._push(test)
        rc = self._drive(test, prompt, art / "driver.log", timeout)
        if rc:
            return rc
        return self._collect(test, art / "facts.json", art / "driver.log")

    def _set_model(self) -> None:
        """Point the lab's baked operator at the run's model, in place.

        why: the model is baked by the lab recipe, so changing it there costs
        a full rebake of the lab — far too much to pay for the ordinary act
        of comparing two models over the same tests. Qwen Code reads its
        model from ~/.qwen/settings.json and ~/.qwen/.env, so rewriting those
        two files after boot makes the model a property of the run instead.
        """
        if self._model_set:
            return
        sets, env = f"{HOME}/.qwen/settings.json", f"{HOME}/.qwen/.env"
        url = shlex.quote(self.base_url) if self.base_url else ""
        out = self.ssh(VM, "\n".join([
            "set -eu",
            f"m={shlex.quote(self.model)}",
            f'python3 - "$m" {url} <<\'PY\'',
            "import json, sys, pathlib",
            "model = sys.argv[1]",
            "base = sys.argv[2] if len(sys.argv) > 2 else ''",
            f"s = pathlib.Path({sets!r})",
            "doc = json.loads(s.read_text())",
            "for p in doc.get('modelProviders', {}).get('openai', []):",
            "    p['id'] = p['name'] = model",
            "    if base:",
            "        p['baseUrl'] = base",
            "doc.setdefault('model', {})['name'] = model",
            "s.write_text(json.dumps(doc, indent=2))",
            f"e = pathlib.Path({env!r})",
            "lines = []",
            "for line in e.read_text().splitlines():",
            "    if line.startswith('OPENAI_MODEL='):",
            "        line = 'OPENAI_MODEL=' + model",
            "    elif base and line.startswith('OPENAI_BASE_URL='):",
            "        line = 'OPENAI_BASE_URL=' + base",
            "    lines.append(line)",
            "e.write_text('\\n'.join(lines) + '\\n')",
            "PY",
            f"chown {USER}:{USER} {sets} {env}",
            f"grep OPENAI_MODEL= {env}",
        ]))
        if f"OPENAI_MODEL={self.model}" not in out:
            raise WorldError(f"{VM}: operator model is not {self.model} after "
                             f"the rewrite (got {out!r})")
        self._model_set = True

    def _push(self, test) -> None:
        """Remove the test's own facts file, ~/.facts/<test>.json, and put
        the test's own files in the operator's home.

        why the payloads: a prompt says "the contents of ~/payload.txt", and
        the operator has a home, not the test directory. object-store-peer
        once stored the payload object-store had left behind, produced that
        object's id, and its oracle went looking on the peer for an object
        that was never put there.

        why that file is removed: the facts are read from it alone, and a
        copy an earlier run of the test left there must not answer for it.
        why only that file: the rest of ~/.facts/ is what earlier sessions
        in this world kept, and bootstrap-user-software-key's file keeps the
        User token for the sessions after it.
        """
        lines = ["set -eu",
                 f"install -d -m 700 -o {USER} -g {USER} {HOME}/.facts",
                 f"rm -f {HOME}/.facts/{test.name}.json"]
        for f in sorted(test.dir.iterdir()):
            if not f.is_file() or f.name in HARNESS_FILES:
                continue
            payload = b64encode(f.read_bytes()).decode()
            lines += [f"printf '%s' '{payload}' | base64 -d > {HOME}/{f.name}",
                      f"chown {USER}:{USER} {HOME}/{f.name}"]
        self.ssh(VM, "\n".join(lines))

    def _drive(self, test, prompt: str, log: Path, timeout: int) -> int:
        """Hand the prompt to the operator and wait; its exit code.

        why the exit code travels as a last line: the world's ssh answers
        with the session's output, and the operator failing is a verdict,
        not a broken transport.
        """
        d = f"{HOME}/.netsim"
        # why base64: the prompt is prose with quotes and newlines, and it
        # travels inside one ssh argv element
        payload = b64encode(prompt.encode()).decode()
        invocation = COMMAND.format(prompt=f"{d}/{test.name}.prompt")
        remote = "\n".join([
            "set -u",
            f'mkdir -p "{d}"',
            f"printf '%s' '{payload}' | base64 -d "
            f'> "{d}/{test.name}.prompt"',
            f'chown -R {USER}:{USER} "{d}"',
            f"if su - {USER} -c {shlex.quote(invocation)} "
            f'> "{d}/{test.name}.log" 2>&1; then rc=0; else rc=$?; fi',
            f'cat "{d}/{test.name}.log"',
            # why its own line: a transcript need not end in a newline
            "echo",
            f'echo "{EXIT}$rc"',
        ])
        # why the tail on both: killing or losing the ssh loses the only copy
        # of the transcript in flight, and "exceeded" or "exit 255" on its
        # own says nothing about whether the operator was working, stuck, or
        # never started. The guest still holds the log it was writing.
        try:
            out = self.ssh(VM, remote, timeout=timeout)
        except subprocess.TimeoutExpired:
            log.write_text(f"[agent] operator exceeded {timeout}s\n"
                           + self._tail(test))
            return 124
        except WorldError as e:
            log.write_text(f"[agent] the session to {VM} broke: {e}\n"
                           + self._tail(test))
            raise
        log.write_text(out + "\n")
        codes = EXIT_LINE.findall(out)
        return int(codes[-1]) if codes else 1

    def _tail(self, test) -> str:
        """The end of the operator's transcript, as the guest holds it."""
        try:
            tail = self.ssh(VM, f"tail -c 4000 {HOME}/.netsim/{test.name}.log "
                                "2>/dev/null || true", check=False)
        except WorldError as e:
            tail = f"(unreadable: {e})"
        return f"[agent] transcript at the cut:\n{tail}\n"

    def _collect(self, test, facts_out: Path, log: Path) -> int:
        """Read the operator's facts from ~/.facts/<test>.json alone; 0 when
        every declared fact is there.

        why one file and only the declared keys: facts used to be a merge of
        every JSON file in the operator's home, so one test's artifact could
        satisfy another test's oracle.
        """
        if not test.facts:
            return 0
        out = self.ssh(VM, f"cat {HOME}/.facts/{test.name}.json "
                           "2>/dev/null || true", check=False)
        try:
            doc = json.loads(out or "null")
        except json.JSONDecodeError:
            doc = None
        missing = (list(test.facts) if not isinstance(doc, dict)
                   else [f for f in test.facts if f not in doc])
        if missing:
            with log.open("a") as f:
                f.write(f"[agent] ~/.facts/{test.name}.json lacks the "
                        f"declared fact(s) {', '.join(missing)}\n")
            return 1
        facts_out.write_text(json.dumps({f: doc[f] for f in test.facts},
                                        indent=2) + "\n")
        return 0
