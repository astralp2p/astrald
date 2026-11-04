"""A VM world: real VMs on a simulated LAN, for one planned world.

Each VM world boots the lab stage the recipe bakes (or the snapshot `--from`
names), pushes the binaries under test to every VM, and closes by pulling
each guest's astrald log and killing the simulation. Nothing is cached
between worlds: a world a later run needs again is saved by name with
`--save` and booted with `--from`, and carries its world record with it.

astrald and astral-query are pushed, never baked. The lab recipe bakes what is
slow and stable — the VMs, the operator, the service unit, the apt
dependencies — and the binaries under test arrive fresh on every boot, so a
stage outlives the commit that filled it.

The session is real ssh port-forwarding. netsim writes a standard OpenSSH
config per simulation at `<sim_dir>/ssh_config` — one `Host <vm>` block, each
`HostName 127.0.0.1` on its own forwarded port, `User root`, an `IdentityFile`
beside it (sourced: netsim `sshutil.render_ssh_config`). So a guest's apphost
reaches the host through `ssh -F <config> -N -L <local>:<socket> <vm>`, an
OpenSSH stream-local forward to apphost's unix socket, and the session view
carries the local end. Drivers and oracles stay byte-identical across envs
because the tunnel is the only difference.

note: none of this has run against a VM since the stage cache went, and the
socket forward never has; the call sequence is pinned in
selftest/test_vm_world.py against a faked netsim.
"""
import asyncio
import json
import os
import shlex
import socket
import subprocess
import sys
import time
from base64 import b64encode
from hashlib import sha256
from pathlib import Path

from lib.build import query_binary
from lib.nodeconfig import config_text, harness_configs
from lib.worlds import World, WorldError, node_entry

TESTS = Path(__file__).resolve().parent.parent.parent
RECIPE = TESTS / "netsim" / "labs" / "two-node-plus-external" / "lab.story"
APPHOST_PORT = 8625          # apphost's TCP listener in the guest
SCRATCH_STAGE = "e2e-scratch"     # a fresh sim always saves; this is what it saves to
GUEST_ROOT = "/var/lib/astrald"   # install-astrald's -root (its systemd unit)
# apphost's unix socket in the guest, beside its TCP listener
APPHOST_SOCKET = f"{GUEST_ROOT}/apphost.sock"
GUEST_TCP_PORT = 1791             # astrald's default tcp listener in the lab
# where a saved world keeps its record, root-only: the operator, `tester`,
# must not read the tokens it is judged with
GUEST_RECORD = "/var/lib/astral-tests/world.json"
# the sshd drop-in that hands root's sessions the VM's harness token
SSHD_DROPIN = "/etc/ssh/sshd_config.d/astral-tests.conf"
SSH_READY_TIMEOUT = 120.0
# why: a guest that just lost its data regenerates its node key and bootstraps
# a fresh onion before apphost listens — minutes on a 1-vCPU VM, not seconds.
APPHOST_READY_TIMEOUT = 300.0
# how long a killed simulation may stay listed before its world fails to close
KILL_TIMEOUT = 60.0


def _netsim(*args, check=True, capture=True, timeout=None) -> str:
    """One netsim invocation. Never inherits a simulation from the shell.
    Past `timeout` seconds it raises subprocess.TimeoutExpired."""
    cmd = ["netsim", *[str(a) for a in args]]
    try:
        p = subprocess.run(cmd, check=check, text=True,
                           capture_output=capture, timeout=timeout)
    except FileNotFoundError as e:
        raise WorldError("netsim is not on PATH") from e
    except subprocess.CalledProcessError as e:
        raise WorldError(
            f"{' '.join(cmd)}: exit {e.returncode}\n{e.stderr or ''}") from e
    return (p.stdout or "").strip()


def recipe_hash(recipe: Path) -> str:
    """Eight hex of the recipe's bytes — the world's shape, not its contents."""
    return sha256(Path(recipe).read_bytes()).hexdigest()[:8]


def lab_stage(recipe: Path) -> str:
    """The stage a recipe bakes into.

    why the hash in the name: a recipe change has to rebake. A fixed name
    stood for every version of the recipe at once, so an edited lab.story
    kept booting the world the old one built.
    """
    return f"e2e-lab-r{recipe_hash(recipe)}"


def stages() -> set:
    """Every stage netsim holds, by name."""
    # netsim names a stage by `slug` (verified: netsim stages --json)
    return {s["slug"] for s in json.loads(_netsim("stages", "--json") or "[]")
            if isinstance(s, dict) and s.get("slug")}


def _sims() -> set:
    """The simulations netsim runs, by id: those that still have VMs.

    why VMs and not the listing: STALE means netsim lost the process that
    owned the simulation, not that the world died — an adopted simulation is
    always STALE and its VMs are still running.
    """
    return {str(s["sim_id"]) for s in
            json.loads(_netsim("ps", "--json") or "[]") if s.get("vms")}


class VmWorld(World):

    def __init__(self, dir: Path, binary: Path, astrald_ref: str,
                 keep: bool = False, config: dict | None = None):
        """dir is the world's own results/<stamp>/w<N>/; config is the node
        config overrides the planner merged for the world (lib/plan.py)."""
        self.dir = Path(dir)
        self.config = config or {}
        self.binary = Path(binary)
        self.astrald_ref = astrald_ref
        self.recipe = RECIPE
        self.stage = None         # a saved world to boot instead of the lab
        self._expected = None     # the record that saved world must hold
        self.saved = None         # the record it does hold, once booted
        self.keep = keep
        self.sim = None
        self._boot_error = None
        self._nodes = {}
        self._tunnels = []
        self.pushed = []

    # --- the runner's interface -------------------------------------------

    def ensure(self, test) -> None:
        self._open()
        # why every VM, not the roster: an op reaches machines no roster
        # names — nat-punch's add-reflector relies on the reflector — and a
        # machine left out keeps the astrald baked with the lab, so the
        # verdict was partly about old code. Once per boot, since a push
        # restarts astrald and the world carries forward between tests.
        self._push_astrald([vm for vm in self._vms() if vm not in self.pushed])
        missing = [n for n in test.nodes if n not in self._nodes]
        if missing:
            raise WorldError(f"{test.name}: the simulation runs no "
                             f"{', '.join(missing)}")
        # why open the session before the ops as well as after: an op is
        # given each node's identity (ASTRAL_ID_<vm>), and until this runs
        # there are no identities to give — leave-lan silently fell back to
        # its alias hunt and failed with the roster in fact correct. Opening
        # twice is what the call is already built for: it reuses a live
        # tunnel, and reopens one whose ssh exited under an op.
        self._open_session(test.nodes)
        for op in test.ops:
            self.run_op(op)
        self._open_session(test.nodes)

    def nodes(self) -> dict:
        """Every VM a roster has named so far, reached through its tunnel.

        The endpoint is the local end of this VM's `ssh -L` forward. Verified
        by hand against a live simulation resumed from the lab stage: a host
        process reached the guest's astrald through the forward and the daemon
        answered at protocol level.

        note: every VM is pushed, but only a roster node has a tunnel.
        """
        return {vm: node_entry(
            endpoint=f"tcp:127.0.0.1:{info['port']}",
            token=info.get("token", ""),
            identity=info.get("identity", ""),
            root=f"netsim:{self.sim}:{vm}",
            tcp_port=GUEST_TCP_PORT,
            lan_endpoint=f"tcp:{self._lan_ip(vm)}:{GUEST_TCP_PORT}",
        ) for vm, info in self._nodes.items() if "port" in info}

    def dead(self) -> list:
        """The roster, if the simulation itself died. A verdict over a dead
        world is void — but a CLI hiccup is not a dead world, so anything
        short of the simulation being gone reports nothing broken and lets
        the oracle speak.
        """
        if self.sim is None:
            return []
        try:
            alive = _sims()
        except WorldError:
            return []
        return [] if str(self.sim) in alive else sorted(self._nodes)

    def close(self, keep: bool) -> None:
        """Pull every guest's astrald log, then end the simulation — or, kept,
        leave it and its tunnels up for whoever debugs it next."""
        if self.sim is None:
            return
        # why the kill in a finally: whatever stops the log pull — a second
        # Ctrl-C, a full disk — must not leave three 2 GiB guests running,
        # nor a simulation every later run refuses to boot beside
        try:
            self._pull_logs()
        finally:
            if not keep:
                for p in self._tunnels:
                    p.terminate()
                self._tunnels.clear()
                # note: kill takes the id positionally — `--sim` is a usage
                # error. Unchecked, since a simulation already reaped answers
                # "no such simulation"; _await_death says whether it is gone.
                # A leaked simulation is not free: a dead one held 6.7 GiB of
                # this host's SD card for two weeks.
                _netsim("kill", self.sim, check=False)
        if keep:
            print(f"run: --keep, simulation {self.sim} left up with its "
                  f"tunnels: netsim ssh --sim {self.sim} <vm>", file=sys.stderr)
            return
        self._await_death(self.sim)

    def _await_death(self, sim: str) -> None:
        """Return once netsim no longer runs simulation `sim`.

        why raise rather than move on: a close that raises ends the run, and
        the next world would otherwise boot beside a simulation still up —
        three more 2 GiB guests, and netsim commands that may land in either.
        """
        deadline = time.monotonic() + KILL_TIMEOUT
        while str(sim) in _sims():
            if time.monotonic() >= deadline:
                raise WorldError(f"simulation {sim} is still up "
                                 f"{KILL_TIMEOUT:.0f} s after netsim kill")
            time.sleep(1.0)

    def boot_from(self, stage: str, record: dict) -> None:
        """Boot the saved world `stage` instead of the lab, and refuse it
        unless its guests hold exactly `record`, the host's copy the run was
        planned from."""
        self.stage, self._expected = stage, record

    def resume(self) -> dict:
        """Boot the saved world, and hand back the record its guests hold."""
        self._open()
        return self.saved

    def ssh(self, vm: str, script: str, check=True, timeout=None) -> str:
        """Run a shell script as root on one of this world's VMs; its output.
        Past `timeout` seconds it raises subprocess.TimeoutExpired."""
        return _netsim("ssh", "--sim", self.sim, vm, "--", script,
                       check=check, timeout=timeout)

    # --- booting -------------------------------------------------------------

    def _open(self) -> None:
        """Boot this world's simulation, once; a boot that failed fails every
        later ensure the same way rather than booting again, and leaves no
        simulation of its own up."""
        if self._boot_error is not None:
            raise WorldError(self._boot_error)
        if self.sim is not None:
            return
        try:
            before = _sims()
            # why refused: netsim attaches commands to the newest simulation,
            # and a world booted beside another — a kept one, another run's,
            # or one an earlier world of this run failed to kill — could run
            # its tests against it
            if before:
                raise WorldError(
                    f"netsim already runs simulation {', '.join(sorted(before))}"
                    ": one VM world at a time — `netsim kill` it, or let its "
                    "run end")
            try:
                self._boot(self.stage or self._base_stage(), before)
            except BaseException:
                self._discard(before)
                raise
            if self.stage:
                self.saved = self._guest_record()
        except WorldError as e:
            self._boot_error = str(e)
            raise

    def _base_stage(self) -> str:
        """The lab every fresh VM world boots, baked once per recipe."""
        name = lab_stage(self.recipe)
        if name not in stages():
            _netsim("story", "--stage", "null", "--save", name,
                    str(self.recipe), capture=False)
        return name

    def _boot(self, stage: str, before: set) -> None:
        """Resume a stage into a simulation and keep it running.

        why: netsim starts a simulation only through `task` or `story`, and
        both need something to run — an empty story is refused outright
        (verified: `error: story has no tasks`). `noop` is the harness's boot
        verb. A fresh simulation always saves, so it saves to a scratch
        stage that is removed straight away.
        """
        _netsim("task", "--stage", stage, "--keep", "--save", SCRATCH_STAGE,
                "noop", capture=False)
        # why the new id and not the newest: nothing else should be up, and
        # an id that appeared is the only one this boot can claim
        new = sorted(_sims() - before)
        _netsim("remove", SCRATCH_STAGE, check=False)
        if len(new) != 1:
            raise WorldError(f"booting {stage}: netsim ps lists new "
                             f"simulations [{', '.join(new)}], not one")
        # note: claimed last, so a boot stopped before this line is _discard's
        self.sim = new[0]

    def _discard(self, before: set) -> None:
        """Kill every simulation a boot that stopped short left up, wait for
        each to go, and remove the scratch stages it may have saved.

        why: under --keep netsim tears nothing down when its task fails, and
        a Ctrl-C kills netsim before it can, so the boot's simulation would
        stay up and every later VM world would refuse to boot beside it.
        why only a lone new one: a netsim task creates exactly one simulation,
        so a lone simulation absent from `before` is this boot's. Two or more
        mean somebody else started one during the boot, and none of them can
        be told apart from this world's, so none is touched.
        why best-effort: the boot's own error, or its Ctrl-C, is what the run
        has to report.
        """
        try:
            new = sorted(_sims() - before)
            if len(new) > 1:
                print(f"run: a stopped boot left simulations {', '.join(new)} "
                      "up; netsim kill the one this run started",
                      file=sys.stderr)
                new = []
            for sim in new:
                _netsim("kill", sim, check=False)
            # note: a task that failed saves its stage under -error
            for name in (SCRATCH_STAGE, f"{SCRATCH_STAGE}-error"):
                _netsim("remove", name, check=False)
            for sim in new:
                self._await_death(sim)
        except WorldError as e:
            print(f"run: a stopped boot left netsim unclean: {e}",
                  file=sys.stderr)

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

    # --- snapshots -----------------------------------------------------------

    def save_world(self, name: str, record: dict) -> str:
        """Put the world record into every guest, root-only, and save the
        simulation as stage `name`; the name netsim saved it under.

        why into the guests: the record is what makes a snapshot usable — its
        history says which tests the world stands past, its facts are what
        those tests learned — and a stage that lost them is the cache this
        replaced, which no fact-reading test could start from.
        """
        payload = b64encode(json.dumps(record, indent=2).encode()).decode()
        for vm in self.pushed:
            self.ssh(vm, f"""set -e
install -d -m 700 {Path(GUEST_RECORD).parent}
umask 077
printf '%s' '{payload}' | base64 -d > {GUEST_RECORD}.new
mv {GUEST_RECORD}.new {GUEST_RECORD}""")
        # note: netsim prints the stage it saved, suffixed -<n> if the name
        # was taken since the run checked it
        out = _netsim("save", "--sim", self.sim, name)
        saved = out.splitlines()[-1].strip() if out else name
        if self.keep:
            # why caught: netsim holds the stage by now, and a save reported
            # as failed leaves it with no host copy, under a name no --from
            # finds and no --save may take
            try:
                self._reopen_tunnels()
            except WorldError as e:
                print(f"run: --keep, saved as {saved}, but a tunnel did not "
                      f"reopen: {e}", file=sys.stderr)
        return saved

    def _reopen_tunnels(self) -> None:
        """A fresh forward to every VM that had one.

        why: `netsim save` shuts the VMs down and relaunches them, so every
        `ssh -L` died with them, and a kept world would point its record at
        dead ports.
        """
        names = [vm for vm, info in self._nodes.items() if "port" in info]
        for vm in names:
            info = self._nodes[vm]
            if info.get("ssh") is not None:
                info["ssh"].terminate()
                if info["ssh"] in self._tunnels:
                    self._tunnels.remove(info["ssh"])
            info.pop("port", None)
        self._open_session(names)

    def _guest_record(self) -> dict:
        """The record the booted saved world's guests hold, which has to be
        the host's copy the run was planned from.

        why equal and not just the same history: the plan, the facts and the
        tokens of a resumed run all come from one record, and a stage saved
        again under the same name — on this host or another — holds another.
        """
        vm = sorted(self._vms())[0] if self._vms() else "node1"
        out = self.ssh(vm, f"cat {GUEST_RECORD}")
        try:
            record = json.loads(out)
        except json.JSONDecodeError as e:
            raise WorldError(f"stage {self.stage}: {vm} holds no world "
                             f"record at {GUEST_RECORD}") from e
        if record != self._expected:
            raise WorldError(
                f"stage {self.stage} holds another world record than the "
                "host's copy the run was planned from: it was saved again "
                "since, or the copy is another stage's")
        return record

    def _pull_logs(self) -> None:
        """Every guest's astrald journal into the world's logs/.

        why best-effort: a log is evidence about a verdict already recorded,
        and a guest too broken to hand it over must not cost the run its
        report.
        """
        logs = self.dir / "logs"
        for vm in self.pushed:
            try:
                out = self.ssh(vm, "journalctl -u astrald --no-pager 2>&1",
                               check=False)
            except WorldError as e:
                out = f"[runner] no journal: {e}"
            try:
                logs.mkdir(parents=True, exist_ok=True)
                (logs / f"{vm}.log").write_text(out + "\n")
            except OSError as e:
                print(f"run: {vm}'s astrald log not kept: {e}",
                      file=sys.stderr)

    def _free_port(self) -> int:
        with socket.socket() as s:
            s.bind(("127.0.0.1", 0))
            return s.getsockname()[1]

    def _tunnel(self, vm: str) -> tuple:
        """A local port that reaches this VM's apphost, held open by ssh -L.

        why the socket and not the TCP port: sshd, and so the forward's far
        end, is in the guest's root netns, and enter-nat moves astrald into
        netns `priv`, whose loopback the root netns cannot reach. A unix
        socket is found by path, and enter-nat leaves the mount namespace
        shared, so one target serves a NAT'd node and a LAN one alike.
        note: sshd connects to the socket anew for each connection, so the
        forward outlives an astrald restart; until apphost listens again, a
        connection through it is closed at once.
        note: stream-local forwarding needs the guest sshd's
        AllowStreamLocalForwarding, on by default; no lab op sets it.
        """
        port = self._free_port()
        argv = ["ssh", "-F", str(self.sim_dir / "ssh_config"), "-N",
                "-o", "ExitOnForwardFailure=yes",
                # why: a guest under load (an astrald restart on one vCPU)
                # can stall long enough for ssh to give up, and a dead
                # forward looks to a driver like a refused connection.
                "-o", "ServerAliveInterval=15", "-o", "ServerAliveCountMax=8",
                "-L", f"{port}:{APPHOST_SOCKET}", vm]
        # why a log file and its own session: a kept world's tunnels outlive
        # the runner, so ssh must neither write into a pipe nobody reads nor
        # die with the runner's Ctrl-C
        self.dir.mkdir(parents=True, exist_ok=True)
        err = (self.dir / f"tunnel-{vm}.log").open("wb")
        proc = subprocess.Popen(argv, stdout=subprocess.DEVNULL, stderr=err,
                                start_new_session=self.keep)
        err.close()
        deadline = time.monotonic() + 20.0
        while time.monotonic() < deadline:
            if proc.poll() is not None:
                text = (self.dir / f"tunnel-{vm}.log").read_text(
                    errors="replace")
                raise WorldError(f"{vm}: ssh -L failed: {text.strip()}")
            with socket.socket() as s:
                s.settimeout(0.5)
                if s.connect_ex(("127.0.0.1", port)) == 0:
                    self._tunnels.append(proc)
                    return port, proc
            time.sleep(0.5)
        proc.terminate()
        raise WorldError(f"{vm}: apphost tunnel never opened on {port}")

    # --- the binary under test ---------------------------------------------

    def _guest_config(self, vm: str, token: str) -> dict:
        """The module config files the push writes into a guest's config dir,
        by file name: the one place a world's config reaches a VM.

        apphost and user are the harness's, as a local node's
        (nodeconfig.harness_configs), with the world's overrides for this VM
        merged key by key; any other module the world overrides gets its table
        alone, since the lab writes no module config of its own.
        note: a saved stage keeps the files its world was pushed, and --from
        refuses a history the manifests now configure otherwise
        (runner._check_saved_config), so a resumed world is pushed them again.
        """
        # why TCP: the guest's own astral-query dials tcp:127.0.0.1:8625 unless
        # told otherwise (astral-go lib/apphost DefaultEndpoint), the vmops'
        # and the operator's alike.
        # why the socket as well: it is what every tunnel reaches (_tunnel).
        # note: the socket is no door for the operator, `tester`. GUEST_ROOT
        # is root-only (install-astrald's `install -d -m 700`), and astrald
        # runs as root under systemd's default umask 022, so the socket is
        # writable by root alone. apphost grants nothing by transport anyway:
        # authority comes from the token a session presents.
        modules = harness_configs([f"tcp:127.0.0.1:{APPHOST_PORT}",
                                   f"unix:{APPHOST_SOCKET}"], token)
        for module, keys in self.config.get(vm, {}).items():
            modules[module] = {**modules.get(module, {}), **keys}
        return {f"{module}.yaml": config_text(doc)
                for module, doc in modules.items()}

    def _push_astrald(self, nodes: list) -> None:
        """Replace each guest's astrald and astral-query with the host-built
        ones, write its config, and restart astrald.

        Guests match the host architecture, so the binary the node-env tests
        ran against is the binary the VM tests run against — one build, one
        ref in the results header. astral-query rides along because the vmops
        speak to astrald through it: a lab-baked one is as old as the stage
        and its queries drift from the astrald under test. The apphost config
        is written here too, so the token in the session view is one the harness
        chose rather than one it had to discover. A snapshot booted with
        `--from` gets new tokens the same way, so none is read from its record.

        why: the value beside a token is the identity it authenticates as, not
        a label. `localnode` is the node itself — an invented name resolves to
        nothing and every call comes back `auth_failed`.
        """
        for vm in nodes:
            token = sha256(f"{vm}:{self.astrald_ref}".encode()).hexdigest()[:32]
            self._scp(vm, self.binary, "/usr/local/bin/astrald")
            self._scp(vm, query_binary(self.binary),
                      "/usr/local/bin/astral-query")
            # why 0600: apphost.yaml names the node's own token, and the
            # operator, `tester`, must not authenticate as the node, nor read
            # the overrides of the world it is judged in. astrald runs as root
            # (install-astrald's unit names no User=), and sshd reads its
            # drop-ins as root.
            files = "".join(
                f"cat > {GUEST_ROOT}/config/{name} <<'EOF'\n{text}EOF\n"
                f"chmod 600 {GUEST_ROOT}/config/{name}\n"
                for name, text in self._guest_config(vm, token).items())
            # why the sshd drop-in: since 77953492 apphost confines a
            # token-less session to anonymous_allowlist, and every vmop runs
            # the guest's astral-query as root with no token — it reads one
            # only from ASTRALD_APPHOST_TOKEN. sshd sets it for root's
            # sessions, so the ops act as the node, while the operator runs
            # under `su -`, whose clean login environment stays token-less.
            # why SetEnv under Match: PermitUserEnvironment is refused in a
            # Match block, and globally it would hand every user's
            # ~/.ssh/environment to sshd.
            # note: `Match all` closes the block, since the drop-in is
            # included at the top of sshd_config.
            self.ssh(vm, f"""set -e
mkdir -p {GUEST_ROOT}/config
{files}cat > {SSHD_DROPIN} <<'EOF'
Match User root
    SetEnv ASTRALD_APPHOST_TOKEN={token}
Match all
EOF
chmod 600 {SSHD_DROPIN}
sshd -t
systemctl reload ssh
systemctl restart astrald""")
            self._nodes[vm] = {"token": token}
            self.pushed.append(vm)

    # --- the session drivers and oracles see ---------------------------------

    def _whoami(self, port: int, token: str) -> str:
        """The identity the apphost behind a tunnel answers with, waited for."""
        import astral

        async def identity_of():
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
            raise WorldError(f"apphost on 127.0.0.1:{port} never answered "
                             f"(last error: {last})")
        return asyncio.run(identity_of())

    def _open_session(self, nodes: list) -> None:
        """Tunnel to each guest's apphost and learn its identity."""
        for vm in nodes:
            info = self._nodes[vm]
            # why: reopen a forward whose ssh has exited. The session view is
            # rewritten per test, so a stale port there is a driver failure
            # that has nothing to do with the test.
            if info.get("ssh") is not None and info["ssh"].poll() is not None:
                info.pop("port", None)
            if "port" not in info:
                info["port"], info["ssh"] = self._tunnel(vm)
            if not info.get("identity"):
                info["identity"] = self._whoami(info["port"], info["token"])

    # --- ops ------------------------------------------------------------------

    def run_op(self, line: str) -> None:
        """`<op> <args…>` — a VM operation and its verifier, host-side."""
        parts = shlex.split(line)
        op, args = parts[0], parts[1:]
        opdir = TESTS / "netsim" / "ops" / op
        if not (opdir / "run.sh").is_file():
            raise WorldError(f"no op {op!r} under netsim/ops")
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
        """Run one vmop, or its verifier, on the host against this simulation."""
        cmd = ([str(script), *argv] if script.suffix == ".sh"
               else [sys.executable, str(script), *argv])
        p = subprocess.run(cmd, env=self._op_env(), capture_output=True,
                           text=True)
        if p.returncode != 0:
            raise WorldError(
                f"{label}: exit {p.returncode}\n{p.stdout or ''}{p.stderr or ''}")

    def _op_env(self) -> dict:
        """The environment a vmop runs in.

        why NETSIM_SIM_DIR: a vmop drives guests from outside — it is a
        host-side script whose own `netsim ssh` and `netsim vm ls` calls have
        to land in THIS simulation, not whichever one happens to be newest.
        That variable is how netsim attaches a command to a simulation (it is
        what `netsim shell` exports), so the environment carries the sim and
        no vmop has to learn a --sim flag of its own.

        why ASTRALPY_SRC: a verifier that talks astral imports the client the
        same way the runner does, from the checkout tests/config.toml names.
        One client for oracles and vmops, never two.

        why ASTRAL_ID_<vm>: a vmop that needs a node's identity used to hunt
        for it in a peer's roster by alias — and an alias is set by whoever
        drove the flow, so `leave-lan` worked under the script driver and died
        under the agent one ("node1 does not know node2 in its swarm roster")
        with the roster in fact correct. The harness learns every identity at
        tunnel time and is the authority on it; an alias is cosmetic and no
        prompt should have to know the harness wants one.
        """
        env = dict(os.environ, NETSIM_SIM_DIR=str(self.sim_dir))
        for vm, info in self._nodes.items():
            if info.get("identity"):
                env[f"ASTRAL_ID_{vm}"] = info["identity"]
        pypath = os.environ.get("ASTRAL_TESTS_PYPATH", "")
        if pypath:
            env["ASTRALPY_SRC"] = pypath.split(os.pathsep)[-1]
        return env

    # --- transport --------------------------------------------------------------

    def _scp(self, vm: str, src: Path, dst: str) -> None:
        """No scp verb in netsim: stream the file through ssh's stdin."""
        cmd = ["netsim", "ssh", "--sim", str(self.sim), vm, "--",
               f"cat > {dst}.new && chmod +x {dst}.new && mv {dst}.new {dst}"]
        with Path(src).open("rb") as f:
            p = subprocess.run(cmd, stdin=f, capture_output=True, text=True)
        if p.returncode != 0:
            raise WorldError(f"{vm}: pushing {src.name}: {p.stderr}")

    def _manifest_vms(self) -> list:
        """The simulation manifest's VM entries."""
        manifest = json.loads((self.sim_dir / "manifest.json").read_text())
        return manifest.get("vms", [])

    def _vms(self) -> list:
        """Every VM the simulation runs, by hostname."""
        return [e["hostname"] for e in self._manifest_vms()
                if e.get("hostname") and e.get("state") == "running"]

    def _lan_ip(self, vm: str) -> str:
        """The VM's address on the simulated LAN, from the sim manifest."""
        for entry in self._manifest_vms():
            if entry.get("hostname") == vm:
                return entry["ip"]
        raise WorldError(f"{vm}: no address in the simulation manifest")
