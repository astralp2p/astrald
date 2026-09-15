"""Render an isolated astrald root's config files (loopback, collision-free).

Every default that two instances would fight over is overridden here:
apphost tcp 8625 / http 8624 / unix socket, tcp listen_port 1791, ether udp
8822, kcp listen_port 1792. ether/kcp get distinct ports instead of a
modules: allowlist — the allowlist would need every module named and
silently rot as astrald grows.

Concurrent runs on one host never share a port: each run leases its own
span of ports before its first node starts.
"""
import fcntl
import socket
import tempfile
from dataclasses import dataclass
from pathlib import Path
from typing import IO

APPHOST_YAML = """\
listen:
  - "tcp:127.0.0.1:{apphost}"
bind_http: ""
tokens:
  "{token}": localnode
"""

TCP_YAML = """\
listen_port: {tcp}
"""

ETHER_YAML = """\
udp_port: {ether}
"""

KCP_YAML = """\
listen_port: {kcp}
"""

# why bound at all: the MCP server is off unless bind_mcp names an endpoint
# (mod/mcp defaults to a single fixed port, which two nodes would fight over).
MCP_YAML = """\
bind_mcp: "tcp:127.0.0.1:{mcp}"
"""


@dataclass
class NodePorts:
    apphost: int
    tcp: int
    ether: int
    kcp: int
    mcp: int


def ports_for(base: int, index: int) -> NodePorts:
    p = base + 10 * index
    return NodePorts(apphost=p, tcp=p + 1, ether=p + 2, kcp=p + 3, mcp=p + 4)


class PortsBusy(Exception):
    """Every span is leased by another run or bound by another process."""


@dataclass
class PortLease:
    """A span of ports one run holds alone, from base upward.

    The lease lasts while `lock` stays open.
    """
    base: int
    lock: IO


def lease_ports(base: int, span: int, spans: int) -> PortLease:
    """Lease the first span that no run holds and no process binds.

    why both checks: the lock keeps two runs that start together off one
    span, and the bind probe skips a span that outlives its run's lock — a
    --keep world, or a process that is not a run at all.
    """
    for start in range(base, base + span * spans, span):
        lock = (Path(tempfile.gettempdir())
                / f"astrald-tests-ports-{start}.lock").open("w")
        try:
            fcntl.flock(lock, fcntl.LOCK_EX | fcntl.LOCK_NB)
        except BlockingIOError:
            lock.close()
            continue
        if all(_bindable(port) for port in range(start, start + span)):
            return PortLease(base=start, lock=lock)
        lock.close()
    raise PortsBusy(f"no free port span: all {spans} spans of {span} ports "
                    f"from {base} are leased or bound")


def _bindable(port: int) -> bool:
    for kind in (socket.SOCK_STREAM, socket.SOCK_DGRAM):
        with socket.socket(socket.AF_INET, kind) as s:
            # why: Go listeners set SO_REUSEADDR, so a port a finished run
            # left in TIME_WAIT is free to astrald and must read free here
            s.setsockopt(socket.SOL_SOCKET, socket.SO_REUSEADDR,
                         kind == socket.SOCK_STREAM)
            try:
                s.bind(("0.0.0.0", port))
            except OSError:
                return False
    return True


def render(root: Path, ports: NodePorts, token: str) -> None:
    cfg = Path(root) / "config"
    cfg.mkdir(parents=True, exist_ok=True)
    (cfg / "apphost.yaml").write_text(
        APPHOST_YAML.format(apphost=ports.apphost, token=token))
    (cfg / "tcp.yaml").write_text(TCP_YAML.format(tcp=ports.tcp))
    (cfg / "ether.yaml").write_text(ETHER_YAML.format(ether=ports.ether))
    (cfg / "kcp.yaml").write_text(KCP_YAML.format(kcp=ports.kcp))
    (cfg / "mcp.yaml").write_text(MCP_YAML.format(mcp=ports.mcp))
