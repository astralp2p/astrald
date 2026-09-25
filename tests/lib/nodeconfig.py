"""Render an isolated astrald root's config files (loopback, collision-free).

Every default that two instances would fight over is overridden here:
apphost tcp 8625 / http 8624 / unix socket, tcp listen_port 1791, ether udp
8822, kcp listen_port 1792. ether/kcp get distinct ports instead of a
modules: allowlist — the allowlist would need every module named and
silently rot as astrald grows.

Concurrent runs on one host never share a port: each run leases its own
span of ports before its first node starts.

Mail between participants is decided by an external authority on a loopback
port of the node's own. Nothing listens there unless a driver serves it, and an
authority that cannot be reached permits nothing.

A node serves MCP unless its roster name is in WITHOUT_MCP. A node that serves
MCP declares the tools in NODE_OP_TOOLS and PEER_TOOLS.
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

# The tools a node serving MCP declares, by the node operation each one puts:
# the operation's path, then the tool's name. mcp-origin calls each as an agent.
#
# why every one answers an agent when the origin guard is removed — the bar for
# membership: shell.shell is an interactive op shell over the whole scope tree;
# mcp.list_agents is mod/mcp's own, and returns every tenant's agent;
# messaging.create_identity is mod/messaging's own, and mints a credential.
#
# why not nodes.new_link: it refuses an argument-less call on its own, so it
# reads the same guarded or not and witnesses nothing.
#
# why every MCP node declares them: a node's config follows its roster name,
# and the node refuses each of these queries for its MCP origin, so they reach
# nothing wherever they are declared.
NODE_OP_TOOLS = {
    "shell.shell": "node_shell",
    "mcp.list_agents": "node_list_agents",
    "messaging.create_identity": "node_create_identity",
}

# The alias every PEER_TOOLS query names as its target. mcp-peer sets it on
# node1 to node2's identity. On a node where nothing sets it, a peer tool answers
# `unknown target`.
PEER_ALIAS = "peer"

# The tools a node serving MCP declares against the node PEER_ALIAS names, by
# the path each one puts: the path, then the tool's name. mcp-peer calls each as
# an agent.
#
# why nodes.links and user.info: the peer answers both to the node the query
# leaves from, and refuses both to a caller holding no permit — the first asks
# mod.auth.admin_network_action, the second mod.user.see_swarm_action. A peer
# answering either to an agent reads the query as the agent's node's.
#
# why test.mcp_peer.caller: it is an app mcp-peer serves on the peer, answering
# the caller the peer read, so the agent's own identity is witnessed arriving.
PEER_TOOLS = {
    "nodes.links": "peer_links",
    "user.info": "peer_user_info",
    "test.mcp_peer.caller": "peer_caller",
}

# why bound at all: the MCP server is off unless bind_mcp names an endpoint
# (mod/mcp defaults to a single fixed port, which two nodes would fight over).
MCP_YAML = """\
bind_mcp: "tcp:127.0.0.1:{mcp}"
tools:
{tools}"""

DECLARED_TOOL_YAML = """\
  - name: {name}
    description: "Put {path} to this node as the agent."
    query: "astral://localnode:{path}"
"""

PEER_TOOL_YAML = """\
  - name: {name}
    description: "Put {path} to the peer node as the agent."
    query: "astral://{alias}:{path}"
"""

# note: an empty bind_mcp turns the MCP server off, and mod/mcp still loads.
MCP_OFF_YAML = """\
bind_mcp: ""
"""

# The roster names whose node serves no MCP.
#
# why a roster name decides it and not a manifest key: a name is one daemon
# for the whole run, shared by every test that names it, so a config chosen by
# the test that happened to start it would change under the tests that follow.
WITHOUT_MCP = frozenset({"nomcp1"})

# why a sweep this short: the node's default is a minute, which is longer than
# most tests live, so an expired external registration would still be in memory
# when the oracle looked. Only the removal and its log line are hurried — the
# fan-out skips an expired registration whatever this is set to.
OBJECTS_YAML = """\
external_registration_sweep_interval: 1s
"""

# why an authority at all: mod/messaging holds no reachability of its own, and
# no handler grants these two actions, so a node without one refuses every
# message. Naming a port a driver may serve lets a test admit one; an unserved
# port refuses, which is what a node configured with none answers.
#
# why mod.messaging.host_mailbox_action is not listed: a node hosts a mailbox
# under the contract the mailbox's identity signs, and auth's chain walk
# answers that without leaving the node.
AUTH_YAML = """\
external_authorizers:
  - endpoint: "{authority_url}"
    actions:
      - mod.messaging.send_action
      - mod.messaging.receive_action
"""


@dataclass
class NodePorts:
    apphost: int
    tcp: int
    ether: int
    kcp: int
    mcp: int
    authority: int

    @property
    def authority_url(self) -> str:
        return f"http://127.0.0.1:{self.authority}/authorize"


def ports_for(base: int, index: int) -> NodePorts:
    p = base + 10 * index
    return NodePorts(apphost=p, tcp=p + 1, ether=p + 2, kcp=p + 3, mcp=p + 4,
                     authority=p + 5)


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


def _mcp_yaml(port: int) -> str:
    tools = "".join(DECLARED_TOOL_YAML.format(name=name, path=path)
                    for path, name in NODE_OP_TOOLS.items())
    tools += "".join(PEER_TOOL_YAML.format(name=name, path=path,
                                           alias=PEER_ALIAS)
                     for path, name in PEER_TOOLS.items())
    return MCP_YAML.format(mcp=port, tools=tools)


def render(root: Path, ports: NodePorts, token: str,
           serves_mcp: bool = True) -> None:
    cfg = Path(root) / "config"
    cfg.mkdir(parents=True, exist_ok=True)
    (cfg / "apphost.yaml").write_text(
        APPHOST_YAML.format(apphost=ports.apphost, token=token))
    (cfg / "tcp.yaml").write_text(TCP_YAML.format(tcp=ports.tcp))
    (cfg / "ether.yaml").write_text(ETHER_YAML.format(ether=ports.ether))
    (cfg / "kcp.yaml").write_text(KCP_YAML.format(kcp=ports.kcp))
    (cfg / "mcp.yaml").write_text(
        _mcp_yaml(ports.mcp) if serves_mcp else MCP_OFF_YAML)
    (cfg / "objects.yaml").write_text(OBJECTS_YAML)
    (cfg / "auth.yaml").write_text(
        AUTH_YAML.format(authority_url=ports.authority_url))
