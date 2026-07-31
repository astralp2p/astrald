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
import asyncio
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
THROWAWAY_STAGE = "e2e-scratch"   # a fresh sim always saves; this is what it saves to
LAB_STAGE = "astrald-lab"         # the recipe's canonical output
GUEST_ROOT = "/var/lib/astrald"   # install-astrald's -root (its systemd unit)
GUEST_TCP_PORT = 1791             # astrald's default tcp listener in the lab
SSH_READY_TIMEOUT = 120.0
# why: a guest that just lost its data regenerates its node key and bootstraps
# a fresh onion before apphost listens — minutes on a 1-vCPU VM, not seconds.
APPHOST_READY_TIMEOUT = 300.0


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
        if test.env == "netsim":
            if not test.steps:
                raise ExecutorError(
                    f"{test.name}: an env netsim test declares its steps")
            if test.steps[-1] != "driver":
                raise ExecutorError(
                    f"{test.name}: `driver` must be the last step, got "
                    f"{test.steps[-1]!r}")
        self._materialize(test.start, test.nodes)
        # why: the roster grows along the chain — bootstrap wants node1,
        # adopt-node wants node1 and node2. Push and tunnel per test, not per
        # boot, or the second node runs the lab's stale astrald with no token.
        self._push_astrald([n for n in test.nodes if n not in self._nodes])
        for step in test.steps[:-1]:
            self._step(step)
        self._open_session(test.nodes)

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
        """The roster, if the simulation itself died. A verdict over a dead
        world is void — but a CLI hiccup is not a dead world, so anything
        short of the simulation being gone reports nothing broken and lets
        the oracle speak.
        """
        if self.sim is None:
            return []
        try:
            sims = json.loads(_netsim("ps", "--json") or "[]")
        except ExecutorError:
            return []
        # why: STALE means netsim lost the process that owned the simulation,
        # not that the world died — an adopted simulation is always STALE and
        # its VMs are still running. A world is broken when netsim no longer
        # lists it, or lists it with no VMs.
        alive = {s["sim_id"] for s in sims if s.get("vms")}
        return [] if str(self.sim) in alive else sorted(self._nodes)

    def teardown(self, keep: bool) -> None:
        for p in self._tunnels:
            p.terminate()
        self._tunnels.clear()
        if keep or self.sim is None:
            return
        _netsim("kill", "--sim", self.sim, check=False)

    # --- materialization ---------------------------------------------------

    def _stages(self) -> set:
        # netsim names a stage by `slug` (verified: netsim stages --json)
        return {s["slug"] for s in
                json.loads(_netsim("stages", "--json") or "[]")
                if isinstance(s, dict) and s.get("slug")}

    def _materialize(self, state: str, nodes: list) -> None:
        """Stand the world up at `state`, from cache when there is one.

        The chain advances inside one simulation, exactly as it advances
        inside one live session on loopback: once a simulation is up, the
        producer tests in the plan carry it forward and nothing re-boots.
        """
        if self.sim is not None:
            self.state = state
            return
        # why: a boot costs minutes and gigabytes. NETSIM_E2E_SIM adopts a
        # simulation that is already up, which is what makes iterating on this
        # executor affordable. A debug affordance: it asserts nothing about
        # what state that simulation stands at.
        adopted = os.environ.get("NETSIM_E2E_SIM")
        if adopted:
            self.sim = adopted
            self.state = state
            return
        key = stage_key(state, self.recipe, self.astrald_ref)
        if key in self._stages():
            self._boot(key)
        else:
            self._boot(self._base_stage())
        self.state = state

    def _base_stage(self) -> str:
        """The lab every chain starts from.

        why: prefer the stage the operator already has. The recipe-keyed name
        is what a fresh bake produces, but an existing `astrald-lab` is the
        same world and costs a resume instead of a twenty-minute build.
        """
        stages = self._stages()
        for name in (f"e2e-lab-r{recipe_hash(self.recipe)}", LAB_STAGE):
            if name in stages:
                return name
        _netsim("story", "--stage", "null", "--save", LAB_STAGE,
                str(self.recipe), capture=False)
        return LAB_STAGE

    def _boot(self, stage: str) -> None:
        """Resume a stage into a simulation and keep it running.

        why: netsim starts a simulation only through `task` or `story`, and
        both need something to run — an empty story is refused outright
        (verified: `error: story has no tasks`). `noop` is the harness's boot
        verb. A fresh simulation always saves, so it saves to a throwaway
        name that `save()` later replaces with the real cache key.
        """
        _netsim("task", "--stage", stage, "--keep", "--save", THROWAWAY_STAGE,
                "noop", capture=False)
        self.sim = self._latest_sim()
        _netsim("remove", THROWAWAY_STAGE, check=False)

    def _latest_sim(self) -> str:
        # netsim keys a simulation by `sim_id` (verified: netsim ps --json)
        sims = json.loads(_netsim("ps", "--json") or "[]")
        if not sims:
            raise ExecutorError("netsim ps lists no simulation after boot")
        return str(sims[-1]["sim_id"])

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

    def _tunnel(self, vm: str) -> tuple:
        """A local port that reaches this VM's apphost, held open by ssh -L."""
        port = self._free_port()
        argv = ["ssh", "-F", str(self.sim_dir / "ssh_config"), "-N",
                "-o", "ExitOnForwardFailure=yes",
                # why: a guest under load (an astrald restart on one vCPU)
                # can stall long enough for ssh to give up, and a dead
                # forward looks to a driver like a refused connection.
                "-o", "ServerAliveInterval=15", "-o", "ServerAliveCountMax=8",
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
                    return port, proc
            time.sleep(0.5)
        proc.terminate()
        raise ExecutorError(f"{vm}: apphost tunnel never opened on {port}")

    # --- the binary under test ---------------------------------------------

    def _push_astrald(self, nodes: list) -> None:
        """Replace each guest's astrald with the host-built one and restart it.

        Guests match the host architecture, so the binary the node-env tests
        ran against is the binary the VM tests run against — one build, one
        ref in the results header. The apphost config is written here too, so
        the token in session.json is one the harness chose rather than one it
        had to discover.

        why: the value beside a token is the identity it authenticates as, not
        a label. `localnode` is the node itself — an invented name resolves to
        nothing and every call comes back `auth_failed`.
        """
        for vm in nodes:
            token = sha256(f"{vm}:{self.astrald_ref}".encode()).hexdigest()[:32]
            self._scp(vm, self.binary, "/usr/local/bin/astrald")
            self._ssh(vm, f"""set -e
mkdir -p {GUEST_ROOT}/config
cat > {GUEST_ROOT}/config/apphost.yaml <<'EOF'
listen:
  - "tcp:127.0.0.1:{APPHOST_PORT}"
bind_http: ""
tokens:
  "{token}": localnode
EOF
systemctl restart astrald""")
            self._nodes[vm] = {"token": token}

    # --- the session drivers and oracles see ---------------------------------

    def _open_session(self, nodes: list) -> None:
        """Tunnel to each guest's apphost, learn its identity, write session.json."""
        import astral

        async def identity_of(port, token):
            deadline = time.monotonic() + APPHOST_READY_TIMEOUT
            last = None
            while time.monotonic() < deadline:
                try:
                    async with await astral.connect(
                            f"tcp:127.0.0.1:{port}", token=token,
                            connect_timeout=3.0) as c:
                        return str(await c.apphost.whoami())
                except Exception as e:               # noqa: BLE001
                    last = e
                    await asyncio.sleep(2)
            raise ExecutorError(f"apphost on 127.0.0.1:{port} never answered "
                                f"(last error: {last})")

        for vm in nodes:
            info = self._nodes[vm]
            # why: reopen a forward whose ssh has exited. session.json is
            # rewritten per test, so a stale port there is a driver failure
            # that has nothing to do with the test.
            if info.get("ssh") is not None and info["ssh"].poll() is not None:
                info.pop("port", None)
            if "port" not in info:
                info["port"], info["ssh"] = self._tunnel(vm)
            if not info.get("identity"):
                info["identity"] = asyncio.run(
                    identity_of(info["port"], info["token"]))
        self._write_session(nodes)

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

    def _lan_ip(self, vm: str) -> str:
        """The VM's address on the simulated LAN, from the sim manifest."""
        manifest = json.loads((self.sim_dir / "manifest.json").read_text())
        for entry in manifest.get("vms", []):
            if entry.get("hostname") == vm:
                return entry["ip"]
        raise ExecutorError(f"{vm}: no address in the simulation manifest")

    def _write_session(self, nodes: list) -> None:
        """session.json — byte-compatible with the local executor's.

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
                "tcp_port": GUEST_TCP_PORT,
                "lan_endpoint": f"tcp:{self._lan_ip(vm)}:{GUEST_TCP_PORT}",
            }
        self.session_json_path.write_text(json.dumps(doc, indent=2) + "\n")
