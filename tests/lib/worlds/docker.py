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
Linux on the engine's architecture (lib/build.linux_binaries). A node reaches
a listener the test serves on the host at the network's gateway (`host_ip`),
and a test reaches a node's files through lib/nodefs.py.

A test with VM operations needs a VM world: netsim's ops do not run here.
"""
import asyncio
import hashlib
import os
import secrets
import shutil
import socket
import struct
import subprocess
import tempfile
import time
from pathlib import Path

from lib.build import query_binary
from lib.nodeconfig import PortSpan, config_text, harness_configs, overlay
from lib.worlds import (World, WorldError, node_entry, refuse_stopped,
                        wait_identity)

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
KEPT = "kept"          # the PID_LABEL of a --keep world, which reap leaves up
# the files the node image is built from; nothing else in CONTEXT counts
IMAGE_FILES = ("Dockerfile", "qwen-settings.sh", "astral-agent.md")
READY_TIMEOUT = 60.0
INFO_TIMEOUT = 10.0
ARCHES = {"x86_64": "amd64", "amd64": "amd64",
          "aarch64": "arm64", "arm64": "arm64"}


def _docker(*args, check=True, timeout=None) -> str:
    """One docker invocation; its stdout. Past `timeout` seconds it raises
    subprocess.TimeoutExpired."""
    cmd = ["docker", *[str(a) for a in args]]
    try:
        # why no stdin: docker exec -i would hand the runner's terminal to
        # the operator, and qwen prepends what it reads there to its prompt
        # why no DOCKER_DEFAULT_PLATFORM: the image and the astrald built for
        # it follow the engine's own architecture (engine_arch)
        p = subprocess.run(cmd, check=check, text=True, capture_output=True,
                           timeout=timeout, stdin=subprocess.DEVNULL,
                           env=_engine_env())
    except FileNotFoundError as e:
        raise WorldError("docker is not on PATH") from e
    except subprocess.CalledProcessError as e:
        raise WorldError(f"docker {args[0]}: exit {e.returncode}\n"
                         f"{(e.stderr or '').strip()}") from e
    return (p.stdout or "").strip()


def _engine_env() -> dict:
    return {k: v for k, v in os.environ.items()
            if k != "DOCKER_DEFAULT_PLATFORM"}


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


def docs_ref() -> str:
    """The astral-docs commit the checkout under test pins in .ai/system,
    which the image's skill carries; read from the tree, so an uninitialized
    submodule still names it."""
    try:
        p = subprocess.run(["git", "-C", str(TESTS.parent), "rev-parse",
                            "HEAD:.ai/system"], check=True, text=True,
                           capture_output=True)
    except (OSError, subprocess.CalledProcessError) as e:
        raise WorldError(f"cannot read the astral-docs pin (.ai/system): "
                         f"{e}") from e
    return p.stdout.strip()


def image_tag() -> str:
    """The node image's tag: a hash of everything it is built from, so an
    edit builds a new image and an unchanged tree reuses the old one."""
    h = hashlib.sha256(QWEN_CODE.encode() + docs_ref().encode())
    for name in IMAGE_FILES:
        h.update(name.encode() + b"\0" + (CONTEXT / name).read_bytes())
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
                            f"QWEN_CODE_VERSION={QWEN_CODE}", "--build-arg",
                            f"ASTRAL_DOCS_REF={docs_ref()}", str(CONTEXT)],
                           stdout=log, stderr=subprocess.STDOUT,
                           stdin=subprocess.DEVNULL, env=_engine_env())
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
        if pid != KEPT and not _alive(pid):
            _docker("rm", "-f", name, check=False)
            gone.append(name)
    networks = _docker("network", "ls", "--filter", f"label={LABEL}",
                       "--format", fmt)
    for line in networks.splitlines():
        name, _, pid = line.partition(" ")
        if pid != KEPT and not _alive(pid):
            _docker("network", "rm", name, check=False)
            gone.append(name)
    return gone


def default_gateway(routes: str) -> str | None:
    """The gateway of the default route in a /proc/net/route table."""
    for line in routes.splitlines()[1:]:
        f = line.split()
        if len(f) > 2 and f[1] == "00000000" and f[2] != "00000000":
            # note: the kernel prints the address as a hex word in its own
            # byte order, little-endian on every engine ARCHES admits
            return socket.inet_ntoa(struct.pack("<I", int(f[2], 16)))
    return None


class DockerWorld(World):

    def __init__(self, dir: Path, binary: Path, image: str, span: PortSpan, *,
                 keep: bool = False, config: dict | None = None,
                 skill: Path | None = None):
        self.dir = Path(dir)
        # the Linux astrald; astral-query lies beside it (build.query_binary)
        self.binary = Path(binary)
        self.image = image
        # why one span for every world of a run: the run holds one lease, and
        # its worlds run one after another, each from node index 0
        # why bounded: node N publishes span.ports(N) on the host, and a node
        # past the span would bind host ports another run may hold
        self.span = span
        self.keep = keep
        self.config = config or {}
        self.skill = Path(skill) if skill else None
        # results/<stamp>/w<N>: the names say which run and world they are
        self.stamp = self.dir.parent.name
        self.prefix = f"astral-{self.stamp}-{self.dir.name}".lower()
        self.network = None
        # the address the containers reach the test host at (gateway)
        self.host_ip = None
        self._nodes = {}
        # names of the nodes `stop` stopped; their containers stay for close
        self._stopped = set()

    # --- the runner's interface -------------------------------------------

    def ensure(self, test) -> None:
        """Bring the test's roster up; a node already up stays as it is."""
        if test.ops:
            raise WorldError(f"{test.name}: a Docker world runs no VM "
                             "operations — they need a VM world (--vm)")
        refuse_stopped(test.name, test.nodes, self._stopped)
        new = [n for n in dict.fromkeys(test.nodes) if n not in self._nodes]
        # why before any start: a refused roster leaves the world as it was
        refusal = self.span.refusal(len(self._nodes), new)
        if refusal:
            raise WorldError(refusal)
        if self.network is None:
            self._open_network()
        for name in new:
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
            host_ip=self.host_ip,
        ) for name, n in self._nodes.items()
            if "identity" in n and name not in self._stopped}

    def restart(self, names: list) -> None:
        """Stop each named node's astrald and start it again on its state."""
        refuse_stopped("restart", names, self._stopped)
        # why every stop first: a node restarted while its siblings still run
        # can relearn what it lost from them, which hides state that a restart
        # of the whole swarm drops (expel-survives-restart); as LocalWorld
        for name in names:
            self._stop_astrald(name)
        for name in names:
            self._run_astrald(name)
            n = self._nodes[name]
            n["identity"] = self._whoami(n["port"], n["token"])
            n["restarts"] += 1

    def stop(self, names: list) -> None:
        """Stop each named node's astrald; its container stays for close to
        copy the log from."""
        for name in names:
            self._stop_astrald(name)
            self._stopped.add(name)

    def dead(self) -> list:
        return [name for name, n in self._nodes.items()
                if name not in self._stopped
                and not self._running(n["container"])]

    def close(self, keep: bool) -> None:
        """Copy each node's log into dir, then remove the containers and the
        network; kept, leave them up."""
        try:
            self._copy_logs()
        finally:
            if not keep:
                self._remove()

    def _copy_logs(self) -> None:
        out = self.dir / "logs"
        out.mkdir(parents=True, exist_ok=True)
        for name, n in self._nodes.items():
            _docker("cp", f"{n['container']}:{ROOT}/astrald.log",
                    out / f"{name}.log", check=False)

    def _remove(self) -> None:
        """Remove the containers and the network, every one whatever another
        raises."""
        stuck = []
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
        return _docker("exec", self._nodes[vm]["container"], "sh",
                       "-c", script, check=check, timeout=timeout)

    def containers(self) -> list:
        """The containers this world started, for --keep's message."""
        return [n["container"] for n in self._nodes.values()]

    # --- nodes ---------------------------------------------------------------

    def _owner(self) -> str:
        """The PID_LABEL value: this runner, or KEPT for a --keep world.

        why: a kept world outlives its runner, and reap removes what a gone
        runner left; kept, it stays up until it is removed by hand
        """
        return KEPT if self.keep else str(os.getpid())

    def _open_network(self) -> None:
        name = f"{self.prefix}-net"
        _docker("network", "create", "--label", f"{LABEL}={self.stamp}",
                "--label", f"{PID_LABEL}={self._owner()}", name)
        self.network = name

    def _start(self, name: str) -> None:
        ports = self.span.ports(len(self._nodes))
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
                "--label", f"{PID_LABEL}={self._owner()}",
                "-p", f"127.0.0.1:{ports.apphost}:{APPHOST_PORT}",
                self.image)
        _docker("cp", self.binary, f"{container}:/usr/local/bin/astrald")
        _docker("cp", query_binary(self.binary),
                f"{container}:/usr/local/bin/astral-query")
        self.ssh(name, self._config_script(name, n["token"], ports.ether))
        if name == OPERATOR_NODE and self.skill:
            self._copy_skill(container)
        self._run_astrald(name)
        if self.host_ip is None:
            self.host_ip = self._gateway(name)
        n["ip"] = _docker("inspect", "-f",
                          "{{(index .NetworkSettings.Networks \""
                          f"{self.network}\").IPAddress}}}}", container)
        n["identity"] = self._whoami(ports.apphost, n["token"])

    def _gateway(self, name: str) -> str:
        """The world network's gateway: the test host's address on it, which
        a listener the test serves binds and a node dials.

        why the default route as the fallback: an engine may leave the IPAM
        gateway unset in `network inspect`, and the route a container sends
        off-network traffic through is that same gateway, read from the
        kernel (/proc/net/route needs no package the image lacks)
        """
        out = _docker("network", "inspect", "-f",
                      "{{range .IPAM.Config}}{{.Gateway}} {{end}}",
                      self.network)
        ipv4 = [a for a in out.split() if "." in a]
        if ipv4:
            return ipv4[0]
        gw = default_gateway(self.ssh(name, "cat /proc/net/route"))
        if gw is None:
            raise WorldError(f"{self.network}: no gateway, in network "
                             f"inspect or in {name}'s routes")
        return gw

    def _config_script(self, name: str, token: str, ether: int) -> str:
        """The script that writes a node's module configs, root-only.

        why 0600: apphost.yaml names the node's own token, which the
        operator, `tester`, must not read — as in a VM (worlds/vm.py).
        why 0.0.0.0: a published port reaches a container's network address,
        never its loopback; the operator's own astral-query still dials the
        default tcp:127.0.0.1:8625.
        """
        # why a UDP port of its own: the containers share one bridge, which
        # carries their nearby broadcasts, and a node that hears a peer's
        # endpoint there learns what a test says only the swarm told it
        # (swarm-roster-sync); local nodes are kept apart the same way
        modules = overlay({**harness_configs([f"tcp:0.0.0.0:{APPHOST_PORT}"],
                                             token),
                           "ether": {"udp_port": ether}},
                          self.config.get(name))
        files = "".join(
            f"cat > {ROOT}/config/{module}.yaml <<'EOF'\n"
            f"{config_text(doc)}EOF\n"
            f"chmod 600 {ROOT}/config/{module}.yaml\n"
            for module, doc in modules.items())
        return f"set -e\ninstall -d -m 700 {ROOT}/config\n{files}"

    def _copy_skill(self, container: str) -> None:
        """Put a host skill's SKILL.md and references over the image's.

        why not its astral-docs: the image carries the docs the astrald
        under test pins, and a host checkout's may be months older
        """
        if not (self.skill / "SKILL.md").is_file():
            raise WorldError(f"{self.skill}: no SKILL.md, so no astral-agent "
                             "skill to give the operator")
        with tempfile.TemporaryDirectory() as tmp:
            copy = Path(tmp) / "astral-agent"
            copy.mkdir()
            shutil.copy(self.skill / "SKILL.md", copy / "SKILL.md")
            if (self.skill / "references").is_dir():
                shutil.copytree(self.skill / "references",
                                copy / "references", symlinks=False)
            self.ssh(OPERATOR_NODE, f"install -d -o tester -g tester "
                                    f"{Path(SKILL_DIR).parent}")
            _docker("cp", f"{copy}/.", f"{container}:{SKILL_DIR}")
        self.ssh(OPERATOR_NODE, f"chown -R tester:tester {SKILL_DIR}")

    def _stop_astrald(self, name: str) -> None:
        # why bounded: an astrald that ignores SIGINT is the world breaking,
        # and the step reads it as environment, not a hang
        self.ssh(name, "pkill -INT -x astrald || true\n"
                       "i=0; while pgrep -x astrald >/dev/null; do\n"
                       "  i=$((i+1)); [ $i -le 150 ] || "
                       "{ echo 'astrald did not stop' >&2; exit 1; }\n"
                       "  sleep 0.2\ndone", timeout=60)

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
        return wait_identity(port, token, READY_TIMEOUT, 1)
