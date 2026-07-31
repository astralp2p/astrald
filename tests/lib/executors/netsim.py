"""env netsim: real VMs on a simulated LAN, materialized from a stage cache.

The expensive part of a VM test is building the world, so this executor never
builds one twice for the same inputs. A state name plus the recipe it came
from plus the astrald ref under test is a cache key; a stage carrying that key
is the same world, and booting it costs a resume instead of a build.

astrald is pushed, never baked. The lab recipe bakes what is slow and stable —
the VMs, the operator, the service unit, the apt dependencies — and the binary
under test arrives fresh on every boot, so a stage outlives the commit that
filled it.

Status: the session tunnel is verified by hand against a live simulation;
the executor as a whole has not yet driven a test end to end.

The session is real ssh port-forwarding. netsim writes a standard OpenSSH
config per simulation at `<sim_dir>/ssh_config` — one `Host <vm>` block, each
`HostName 127.0.0.1` on its own forwarded port, `User root`, an `IdentityFile`
beside it (sourced: netsim `sshutil.render_ssh_config`). So a guest's apphost
reaches the host through `ssh -F <config> -N -L <local>:127.0.0.1:8625 <vm>`,
and `session.json` carries the local end. Drivers and oracles stay
byte-identical across envs because the tunnel is the only difference.
"""
import json
import os
import shlex
import socket
import subprocess
import time
from hashlib import sha256
from pathlib import Path

from lib.executors import Executor, ExecutorError

TESTS = Path(__file__).resolve().parent.parent.parent
APPHOST_PORT = 8625          # the guest's apphost, inside netns priv when NAT'd
THROWAWAY_STAGE = "e2e-scratch"   # a story always saves; this is what it saves to
SSH_READY_TIMEOUT = 120.0


def _netsim(*args, check=True, capture=True) -> str:
    """One netsim invocation. Never inherits a simulation from the shell."""
    cmd = ["netsim", *[str(a) for a in args]]
    try:
        p = subprocess.run(cmd, check=check, text=True,
                           capture_output=capture)
    except FileNotFoundError as e:
        raise ExecutorError("netsim is not on PATH") from e
    except subprocess.CalledProcessError as e:
        raise ExecutorError(
            f"{' '.join(cmd)}: exit {e.returncode}\n{e.stderr or ''}") from e
    return (p.stdout or "").strip()


def recipe_hash(recipe: Path) -> str:
    """Eight hex of the recipe's bytes — the world's shape, not its contents."""
    return sha256(Path(recipe).read_bytes()).hexdigest()[:8]


def stage_key(state: str, recipe: Path, astrald_ref: str) -> str:
    """The cache key a stage carries.

    why: a state name alone is not the world. The same `two-nodes` built from
    a different recipe, or filled by a different astrald, is a different thing
    to test against, and reusing it would report a verdict about a binary
    nobody asked about.
    """
    return f"e2e-{state}-r{recipe_hash(recipe)}-g{astrald_ref}"


class NetsimExecutor(Executor):
    env = "netsim"

    def __init__(self, dir: Path, binary: Path, astrald_ref: str):
        self.dir = Path(dir)
        self.dir.mkdir(parents=True, exist_ok=True)
        self.binary = Path(binary)
        self.astrald_ref = astrald_ref
        self.recipe = TESTS / "fixtures" / "lab" / "lab.story"
        self.sim = None
        self.state = None
        self.facts = {}
        self._nodes = {}
        self._tunnels = []

    # --- the runner's interface -------------------------------------------

    @property
    def session_json_path(self) -> Path:
        return self.dir / "session.json"

    def prepare(self, test) -> None:
        if not test.steps:
            raise ExecutorError(
                f"{test.name}: an env netsim test declares its steps")
        if test.steps[-1] != "driver":
            # why: the runner owns the driver, so the executor can only run the
            # steps that precede it. Every shipped netsim test ends with
            # `driver`; a step after it would silently never run.
            raise ExecutorError(
                f"{test.name}: `driver` must be the last step, got "
                f"{test.steps[-1]!r}")

        self._materialize(test.start, test.nodes)
        for step in test.steps[:-1]:
            self._step(step)
        self._write_session(test.nodes)

    def merge_facts(self, path: Path) -> None:
        if Path(path).exists():
            self.facts.update(json.loads(Path(path).read_text()))
            self._write_session(list(self._nodes))

    def save(self, state: str) -> None:
        """Snapshot the running simulation under this state's cache key.

        Probe first and never resave: `netsim save` suffixes a taken name
        rather than replacing it, so a blind save grows `-1`, `-2`, … stages
        that no probe will ever match again (migration plan, decision 6).
        """
        key = stage_key(state, self.recipe, self.astrald_ref)
        if key in self._stages():
            return
        _netsim("save", "--sim", self.sim, key)
        self.state = state

    def broken(self) -> list:
        """VMs that stopped answering. A verdict over a dead VM is void."""
        if self.sim is None:
            return []
        try:
            vms = json.loads(_netsim("vm", "ls", "--json", "--sim", self.sim))
        except ExecutorError:
            return sorted(self._nodes)
        return sorted(v["hostname"] for v in vms
                      if v.get("hostname") in self._nodes
                      and v.get("state") != "running")

    def teardown(self, keep: bool) -> None:
        for p in self._tunnels:
            p.terminate()
        self._tunnels.clear()
        if keep or self.sim is None:
            return
        _netsim("kill", "--sim", self.sim, check=False)

    # --- materialization ---------------------------------------------------

    def _stages(self) -> set:
        return {s["name"] if isinstance(s, dict) else str(s)
                for s in json.loads(_netsim("stages", "--json") or "[]")}

    def _materialize(self, state: str, nodes: list) -> None:
        """Bring up a simulation standing at `state`, from cache when possible."""
        if self.sim is not None and self.state == state:
            return
        key = stage_key(state, self.recipe, self.astrald_ref)
        if key in self._stages():
            self._boot(key)
        elif state == "null":
            self._build(nodes)
        else:
            # The plan's producer for this state was expected to run first and
            # call save(). Reaching here means the selection skipped it.
            raise ExecutorError(
                f"no cached stage {key!r} and no producer ran for state "
                f"{state!r} — run the test that saves it, or the suite that "
                "lists it, so the chain gets built")
        self.state = state
        self._push_astrald(nodes)

    def _boot(self, key: str, steps=()) -> None:
        """Resume a cached stage, run the test's vm steps, keep it running.

        why: one `netsim story` rather than a boot verb plus per-step calls.
        netsim rejects a story with no tasks — verified: `error: story has no
        tasks` — so there is no empty-story boot, and a story is the unit the
        tool actually offers. A story on a fresh simulation always saves, so
        it saves to a throwaway name that `save()` later replaces with the
        real cache key.
        """
        story = self.dir / "steps.story"
        story.write_text("".join(f"{s}\n" for s in steps)
                         or "# no vm steps\n")
        if not steps:
            raise ExecutorError(
                "a netsim test with no vm: steps cannot boot — netsim has no "
                "boot-only verb and rejects an empty story")
        _netsim("story", "--stage", key, "--keep", "--save", THROWAWAY_STAGE,
                str(story))
        self.sim = self._latest_sim()
        _netsim("remove", THROWAWAY_STAGE, check=False)

    def _build(self, nodes: list) -> None:
        """Boot the lab base and let the runner's plan fill the chain.

        why: the executor never walks the chain itself. The runner already
        resolved the producer tests into the plan and calls `save` after each
        one, so a cache miss only has to supply an empty world of the right
        shape — the same e2e tests that build `two-nodes` on loopback build it
        here, which is what keeps one definition of a state across both envs.
        """
        base = f"e2e-lab-r{recipe_hash(self.recipe)}"
        if base not in self._stages():
            _netsim("story", "--stage", "null", "--save", base,
                    str(self.recipe))
        self._boot(base)

    def _latest_sim(self) -> str:
        sims = json.loads(_netsim("ps", "--json") or "[]")
        if not sims:
            raise ExecutorError("netsim ps lists no simulation after boot")
        return str(sims[-1]["id"])

    @property
    def sim_dir(self) -> Path:
        """<netsim home>/sims/<id> — where ssh_config and the key live.

        Resolved the way netsim resolves it (config.home): NETSIM_HOME, then
        XDG_DATA_HOME/netsim, then ~/.local/share/netsim.
        """
        env = os.environ.get("NETSIM_HOME")
        if env:
            home = Path(env).expanduser()
        else:
            xdg = os.environ.get("XDG_DATA_HOME")
            base = Path(xdg).expanduser() if xdg else Path.home() / ".local" / "share"
            home = base / "netsim"
        return home / "sims" / str(self.sim)

    def _free_port(self) -> int:
        with socket.socket() as s:
            s.bind(("127.0.0.1", 0))
            return s.getsockname()[1]

    def _tunnel(self, vm: str) -> int:
        """A local port that reaches this VM's apphost, held open by ssh -L."""
        port = self._free_port()
        argv = ["ssh", "-F", str(self.sim_dir / "ssh_config"), "-N",
                "-o", "ExitOnForwardFailure=yes",
                "-L", f"{port}:127.0.0.1:{APPHOST_PORT}", vm]
        proc = subprocess.Popen(argv, stdout=subprocess.DEVNULL,
                                stderr=subprocess.PIPE)
        deadline = time.monotonic() + 20.0
        while time.monotonic() < deadline:
            if proc.poll() is not None:
                err = (proc.stderr.read() or b"").decode(errors="replace")
                raise ExecutorError(f"{vm}: ssh -L failed: {err.strip()}")
            with socket.socket() as s:
                s.settimeout(0.5)
                if s.connect_ex(("127.0.0.1", port)) == 0:
                    self._tunnels.append(proc)
                    return port
            time.sleep(0.5)
        proc.terminate()
        raise ExecutorError(f"{vm}: apphost tunnel never opened on {port}")

    # --- the binary under test ---------------------------------------------

    def _push_astrald(self, nodes: list) -> None:
        """Replace the guest's astrald with the host-built one and restart it.

        Guests match the host architecture, so the binary the node-env tests
        ran against is the binary the VM tests run against — one build, one
        ref in the results header.
        """
        for vm in nodes:
            token = sha256(f"{vm}:{self.astrald_ref}".encode()).hexdigest()[:32]
            self._scp(vm, self.binary, "/usr/local/bin/astrald")
            self._ssh(vm, "mkdir -p /root/.config/astrald && "
                          "cat > /root/.config/astrald/apphost.yaml <<'EOF'\n"
                          "listen:\n"
                          f'  - "tcp:127.0.0.1:{APPHOST_PORT}"\n'
                          'bind_http: ""\n'
                          "tokens:\n"
                          f'  "{token}": e2e\n'
                          "EOF\n"
                          "systemctl restart astrald")
            self._nodes[vm] = {"token": token}
        for vm in nodes:
            self._wait_ready(vm)

    def _wait_ready(self, vm: str) -> None:
        deadline = time.monotonic() + SSH_READY_TIMEOUT
        while time.monotonic() < deadline:
            out = self._ssh(vm, "astral-query localnode:.spec >/dev/null 2>&1 "
                                "&& echo ready", check=False)
            if "ready" in out:
                return
            time.sleep(2)
        raise ExecutorError(f"{vm}: astrald did not answer after "
                            f"{SSH_READY_TIMEOUT}s")

    # --- steps --------------------------------------------------------------

    def _step(self, step: str) -> None:
        """`vm:<op> <args…>` — a vmop and its verifier, host-side."""
        kind, _, rest = step.partition(":")
        if kind != "vm":
            raise ExecutorError(f"unknown step {step!r} (vm:<op> … | driver)")
        parts = shlex.split(rest)
        op, args = parts[0], parts[1:]
        opdir = TESTS / "fixtures" / "vmops" / op
        if not (opdir / "run.sh").is_file():
            raise ExecutorError(f"no vmop {op!r} under fixtures/vmops")
        # A bare word names a VM; anything flag-shaped passes through, so a
        # vmop with its own options (`leave-lan --peer node1`) stays writable.
        argv, passthrough = [], False
        for a in args:
            if a.startswith("-"):
                passthrough = True
            argv += [a] if passthrough else ["--vm", a]
        self._vmop(opdir / "run.sh", argv, op)
        for verifier in ("verify.sh", "verify.py"):
            if (opdir / verifier).is_file():
                self._vmop(opdir / verifier, argv, f"{op}:{verifier}")
                break

    def _vmop(self, script: Path, argv: list, label: str) -> None:
        cmd = [str(script), *argv]
        if script.suffix == ".py":
            cmd.insert(0, "python3")
        p = subprocess.run(cmd, text=True, capture_output=True,
                           env=self._sim_env())
        if p.returncode != 0:
            raise ExecutorError(
                f"{label}: exit {p.returncode}\n{p.stdout}\n{p.stderr}")

    def _sim_env(self) -> dict:
        import os
        return dict(os.environ, NETSIM_SIM_ID=str(self.sim))

    # --- the session drivers and oracles see --------------------------------

    def _ssh(self, vm: str, script: str, check=True) -> str:
        return _netsim("ssh", "--sim", self.sim, vm, "--", script, check=check)

    def _scp(self, vm: str, src: Path, dst: str) -> None:
        """No scp verb in netsim: stream the file through ssh's stdin."""
        cmd = ["netsim", "ssh", "--sim", str(self.sim), vm, "--",
               f"cat > {dst}.new && chmod +x {dst}.new && mv {dst}.new {dst}"]
        with Path(src).open("rb") as f:
            p = subprocess.run(cmd, stdin=f, capture_output=True, text=True)
        if p.returncode != 0:
            raise ExecutorError(f"{vm}: pushing {src.name}: {p.stderr}")

    def _write_session(self, nodes: list) -> None:
        """session.json, byte-compatible with the local executor's.

        The endpoint is the local end of this VM's `ssh -L` forward. Verified
        by hand against a live simulation resumed from `astrald-lab`: a host
        process reached the guest's astrald through the forward and the daemon
        answered at protocol level.

        fixme: a NAT'd node's apphost listens inside netns `priv`, which
        `ssh -L` cannot reach — it lands in the VM's default namespace. That
        affects `nat-punch` and not `tor-link`. The fix is a relay inside the
        guest, published by `enter-nat`, so the forward stays uniform.
        """
        doc = {"nodes": {}, "facts": self.facts}
        for vm in nodes:
            info = self._nodes.get(vm, {})
            doc["nodes"][vm] = {
                "endpoint": f"tcp:127.0.0.1:{info.get('port', 0)}",
                "token": info.get("token", ""),
                "identity": info.get("identity", ""),
                "root": f"netsim:{self.sim}:{vm}",
                "tcp_port": 1791,
            }
        self.session_json_path.write_text(json.dumps(doc, indent=2) + "\n")
