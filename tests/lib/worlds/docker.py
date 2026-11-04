"""A Docker world: one throwaway container per roster node, on one network.

It is the lab's shape without netsim. Each node is a machine of its own, the
nodes share a LAN (the world's Docker network), and node1 holds the lab's
operator, `tester` with Qwen Code (tests/docker/Dockerfile). `ssh()` keeps
VmWorld.ssh's contract over `docker exec`, so lib/agent.py drives node1's
container with the scripts it sends a VM.

A node's apphost is published on the host's 127.0.0.1, at the port the run's
lease gives that node, so drivers and oracles reach it as they reach a local
node. Nothing of the host enters a container: no bind mount and no
environment variable. The binaries under test are copied in, built for
Linux on the engine's architecture (lib/build.linux_binaries).

A test with VM operations needs a VM world: netsim's ops do not run here.
"""
import asyncio
import hashlib
import os
import secrets
import shutil
import subprocess
import tempfile
import time
from pathlib import Path

from lib.build import query_binary
from lib.nodeconfig import config_text, harness_configs, ports_for
from lib.worlds import World, WorldError, node_entry

TESTS = Path(__file__).resolve().parent.parent.parent
CONTEXT = TESTS / "docker"            # the image's build context
BUILD_LOG = TESTS / ".cache" / "docker-build.log"
QWEN_CODE = "0.26.7"                  # the operator's version, pinned
ROOT = "/var/lib/astrald"             # astrald's -root in a container
APPHOST_PORT = 8625                   # apphost's TCP listener in a container
TCP_PORT = 1791                       # astrald's default tcp listener
OPERATOR_NODE = "node1"               # where the lab puts the operator
SKILL_DIR = "/home/tester/.qwen/skills/astral-agent"
# labels every container and network of a run carries, so a run that died
# without closing its world is cleaned up by the next one (reap)
LABEL = "astral.tests.run"
PID_LABEL = "astral.tests.pid"
READY_TIMEOUT = 60.0
INFO_TIMEOUT = 10.0
ARCHES = {"x86_64": "amd64", "amd64": "amd64",
          "aarch64": "arm64", "arm64": "arm64"}


def _docker(*args, check=True, timeout=None) -> str:
    """One docker invocation; its stdout. Past `timeout` seconds it raises
    subprocess.TimeoutExpired."""
    cmd = ["docker", *[str(a) for a in args]]
    try:
        p = subprocess.run(cmd, check=check, text=True, capture_output=True,
                           timeout=timeout)
    except FileNotFoundError as e:
        raise WorldError("docker is not on PATH") from e
    except subprocess.CalledProcessError as e:
        raise WorldError(f"docker {args[0]}: exit {e.returncode}\n"
                         f"{(e.stderr or '').strip()}") from e
    return (p.stdout or "").strip()


def unavailable() -> str | None:
    """Why this host can run no Docker world, or None when it can."""
    if shutil.which("docker") is None:
        return "docker is not on PATH"
    try:
        _docker("info", "--format", "{{.ServerVersion}}",
                timeout=INFO_TIMEOUT)
    except subprocess.TimeoutExpired:
        return f"the Docker engine did not answer within {INFO_TIMEOUT:.0f} s"
    except WorldError as e:
        last = str(e).strip().splitlines()[-1]
        return f"the Docker engine does not answer ({last})"
    return None


def engine_arch() -> str:
    """The GOARCH of the engine's machines, which a container runs on."""
    machine = _docker("info", "--format", "{{.Architecture}}",
                      timeout=INFO_TIMEOUT)
    if machine not in ARCHES:
        raise WorldError(f"the Docker engine runs {machine!r}, which this "
                         "harness builds no astrald for")
    return ARCHES[machine]


def image_tag() -> str:
    """The node image's tag: a hash of everything it is built from, so an
    edit builds a new image and an unchanged tree reuses the old one."""
    h = hashlib.sha256(QWEN_CODE.encode())
    for f in sorted(CONTEXT.iterdir()):
        h.update(f.name.encode() + b"\0" + f.read_bytes())
    return f"astral-node:{h.hexdigest()[:12]}"


def ensure_image() -> str:
    """The node image's tag, built first when the engine lacks it."""
    tag = image_tag()
    try:
        _docker("image", "inspect", tag)
        return tag
    except WorldError:
        pass
    BUILD_LOG.parent.mkdir(parents=True, exist_ok=True)
    with BUILD_LOG.open("w") as log:
        p = subprocess.run(["docker", "build", "-t", tag, "--build-arg",
                            f"QWEN_CODE_VERSION={QWEN_CODE}", str(CONTEXT)],
                           stdout=log, stderr=subprocess.STDOUT)
    if p.returncode != 0:
        raise WorldError(f"the node image failed to build: {BUILD_LOG}")
    return tag


def _alive(pid: str) -> bool:
    try:
        os.kill(int(pid), 0)
    except (ValueError, ProcessLookupError):
        return False
    except PermissionError:
        return True
    return True


def reap() -> list:
    """Remove the containers and networks of runs that are gone; their
    names.

    why: a runner killed by SIGKILL never closes its world, and a container
    outlives it, holding a published port the next lease may hand out.
    """
    gone = []
    fmt = '{{.Name}} {{.Label "' + PID_LABEL + '"}}'
    containers = _docker("ps", "-a", "--filter", f"label={LABEL}",
                         "--format", fmt.replace(".Name", ".Names"))
    for line in containers.splitlines():
        name, _, pid = line.partition(" ")
        if not _alive(pid):
            _docker("rm", "-f", name, check=False)
            gone.append(name)
    networks = _docker("network", "ls", "--filter", f"label={LABEL}",
                       "--format", fmt)
    for line in networks.splitlines():
        name, _, pid = line.partition(" ")
        if not _alive(pid):
            _docker("network", "rm", name, check=False)
            gone.append(name)
    return gone


class DockerWorld(World):

    def __init__(self, dir: Path, binary: Path, image: str, port_base: int,
                 keep: bool = False, config: dict | None = None,
                 skill: Path | None = None):
        self.dir = Path(dir)
        # the Linux astrald; astral-query lies beside it (build.query_binary)
        self.binary = Path(binary)
        self.image = image
        # why one base for every world of a run: the run holds one lease, and
        # its worlds run one after another, each from node index 0
        self.port_base = port_base
        self.keep = keep
        self.config = config or {}
        self.skill = Path(skill) if skill else None
        # results/<stamp>/w<N>: the names say which run and world they are
        self.stamp = self.dir.parent.name
        self.prefix = f"astral-{self.stamp}-{self.dir.name}".lower()
        self.network = None
        self._nodes = {}

    # --- the runner's interface -------------------------------------------

    def ensure(self, test) -> None:
        """Bring the test's roster up; a node already up stays as it is."""
        if test.ops:
            raise WorldError(f"{test.name}: a Docker world runs no VM "
                             "operations — they need a VM world (--vm)")
        if self.network is None:
            self._open_network()
        for name in test.nodes:
            if name not in self._nodes:
                self._start(name)

    def nodes(self) -> dict:
        return {name: node_entry(
            endpoint=f"tcp:127.0.0.1:{n['port']}",
            token=n["token"],
            identity=n.get("identity", ""),
            root=f"docker:{n['container']}:{ROOT}",
            tcp_port=TCP_PORT,
            # why the container's address: it is what a peer on the world's
            # network dials, which the host's published port is not
            lan_endpoint=f"tcp:{n.get('ip', '')}:{TCP_PORT}",
            restarts=n["restarts"],
        ) for name, n in self._nodes.items() if "identity" in n}

    def restart(self, names: list) -> None:
        """Stop each named node's astrald and start it again on its state."""
        for name in names:
            self.ssh(name, "pkill -INT -x astrald || true\n"
                           "while pgrep -x astrald >/dev/null; do sleep 0.2; done")
            self._run_astrald(name)
            n = self._nodes[name]
            n["identity"] = self._whoami(n["port"], n["token"])
            n["restarts"] += 1

    def dead(self) -> list:
        return [name for name, n in self._nodes.items()
                if not self._running(n["container"])]

    def close(self, keep: bool) -> None:
        """Copy each node's log into dir, then remove the containers and the
        network; kept, leave them up."""
        stuck = []
        for name, n in self._nodes.items():
            out = self.dir / "logs"
            out.mkdir(parents=True, exist_ok=True)
            _docker("cp", f"{n['container']}:{ROOT}/astrald.log",
                    out / f"{name}.log", check=False)
        if keep:
            return
        for name, n in self._nodes.items():
            try:
                _docker("rm", "-f", n["container"])
            except WorldError as e:
                stuck.append(f"{name}: {e}")
        if self.network is not None:
            try:
                _docker("network", "rm", self.network)
            except WorldError as e:
                stuck.append(f"network: {e}")
        if stuck:
            raise WorldError(f"close: {'; '.join(stuck)}")

    def ssh(self, vm: str, script: str, check=True, timeout=None) -> str:
        """Run a shell script as root in one node's container; its output.
        Past `timeout` seconds it raises subprocess.TimeoutExpired."""
        if vm not in self._nodes:
            raise WorldError(f"the Docker world runs no {vm}")
        return _docker("exec", "-i", self._nodes[vm]["container"], "sh",
                       "-c", script, check=check, timeout=timeout)

    def containers(self) -> list:
        """The containers this world started, for --keep's message."""
        return [n["container"] for n in self._nodes.values()]

    # --- nodes ---------------------------------------------------------------

    def _open_network(self) -> None:
        name = f"{self.prefix}-net"
        _docker("network", "create", "--label", f"{LABEL}={self.stamp}",
                "--label", f"{PID_LABEL}={os.getpid()}", name)
        self.network = name

    def _start(self, name: str) -> None:
        ports = ports_for(self.port_base, len(self._nodes))
        container = f"{self.prefix}-{name}"
        # why tracked before the container exists: a node that fails while
        # starting must still be removed by close
        n = self._nodes[name] = {"container": container,
                                 "port": ports.apphost,
                                 "token": secrets.token_hex(16),
                                 "restarts": 0}
        # why no -v and no -e: nothing of the host — files, environment —
        # may reach a container the operator works in
        _docker("run", "-d", "--init", "--name", container,
                "--hostname", name, "--network", self.network,
                "--label", f"{LABEL}={self.stamp}",
                "--label", f"{PID_LABEL}={os.getpid()}",
                "-p", f"127.0.0.1:{ports.apphost}:{APPHOST_PORT}",
                self.image)
        _docker("cp", self.binary, f"{container}:/usr/local/bin/astrald")
        _docker("cp", query_binary(self.binary),
                f"{container}:/usr/local/bin/astral-query")
        self.ssh(name, self._config_script(name, n["token"]))
        if name == OPERATOR_NODE and self.skill:
            self._copy_skill(container)
        self._run_astrald(name)
        n["ip"] = _docker("inspect", "-f",
                          "{{(index .NetworkSettings.Networks \""
                          f"{self.network}\").IPAddress}}}}", container)
        n["identity"] = self._whoami(ports.apphost, n["token"])

    def _config_script(self, name: str, token: str) -> str:
        """The script that writes a node's module configs, root-only.

        why 0600: apphost.yaml names the node's own token, which the
        operator, `tester`, must not read — as in a VM (worlds/vm.py).
        why 0.0.0.0: a published port reaches a container's network address,
        never its loopback; the operator's own astral-query still dials the
        default tcp:127.0.0.1:8625.
        """
        modules = harness_configs([f"tcp:0.0.0.0:{APPHOST_PORT}"], token)
        for module, keys in self.config.get(name, {}).items():
            modules[module] = {**modules.get(module, {}), **keys}
        files = "".join(
            f"cat > {ROOT}/config/{module}.yaml <<'EOF'\n"
            f"{config_text(doc)}EOF\n"
            f"chmod 600 {ROOT}/config/{module}.yaml\n"
            for module, doc in modules.items())
        return f"set -e\ninstall -d -m 700 {ROOT}/config\n{files}"

    def _copy_skill(self, container: str) -> None:
        """Copy the astral-agent skill to the operator, links resolved."""
        if not (self.skill / "SKILL.md").is_file():
            raise WorldError(f"{self.skill}: no SKILL.md, so no astral-agent "
                             "skill to give the operator")
        with tempfile.TemporaryDirectory() as tmp:
            copy = Path(tmp) / "astral-agent"
            shutil.copytree(self.skill, copy, symlinks=False)
            self.ssh(OPERATOR_NODE, f"install -d -o tester -g tester "
                                    f"{Path(SKILL_DIR).parent}")
            _docker("cp", f"{copy}/.", f"{container}:{SKILL_DIR}")
        self.ssh(OPERATOR_NODE, f"chown -R tester:tester {SKILL_DIR}")

    def _run_astrald(self, name: str) -> None:
        _docker("exec", "-d", self._nodes[name]["container"], "sh", "-c",
                f"exec astrald -root {ROOT} >>{ROOT}/astrald.log 2>&1")

    def _running(self, container: str) -> bool:
        try:
            _docker("exec", container, "pgrep", "-x", "astrald")
        except WorldError:
            return False
        return True

    def _whoami(self, port: int, token: str) -> str:
        """The identity the apphost behind a published port answers with."""
        import astral

        async def identity_of():
            deadline = time.monotonic() + READY_TIMEOUT
            last = None
            while time.monotonic() < deadline:
                try:
                    async with await astral.connect(
                            f"tcp:127.0.0.1:{port}", token=token,
                            connect_timeout=3.0) as c:
                        return str(await c.apphost.whoami())
                except Exception as e:               # noqa: BLE001
                    last = e
                    await asyncio.sleep(1)
            raise WorldError(f"apphost on 127.0.0.1:{port} never answered "
                             f"(last error: {last})")
        return asyncio.run(identity_of())
