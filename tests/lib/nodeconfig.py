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

Every node serves MCP and declares the tools in NODE_OP_TOOLS and PEER_TOOLS.
A test changes any of this for its world with a `[config.<node>.<module>]`
override, merged key by key on top of what is built here.
"""
import fcntl
import json
import socket
import tempfile
from dataclasses import dataclass
from pathlib import Path
from typing import IO

# The tools a node serving MCP declares, by the node operation each one puts:
# the operation's path, then the tool's name. mcp-origin calls each as an agent.
#
# why every one answers an agent when the origin guard is removed — the bar for
# membership: shell.shell is an interactive op shell over the whole scope tree;
# mcp.list_agents is mod/mcp's own, and returns every tenant's agent;
# messaging.create_mailbox is mod/messaging's own, and mints a credential.
#
# why not nodes.new_link: it refuses an argument-less call on its own, so it
# reads the same guarded or not and witnesses nothing.
#
# why every MCP node declares them: the node refuses each of these queries for
# its MCP origin, so they reach nothing wherever they are declared.
NODE_OP_TOOLS = {
    "shell.shell": "node_shell",
    "mcp.list_agents": "node_list_agents",
    "messaging.create_mailbox": "node_create_mailbox",
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

# why a sweep this short: the node's default is a minute, which is longer than
# most tests live, so an expired external registration would still be in memory
# when the oracle looked. Only the removal and its log line are hurried — the
# fan-out skips an expired registration whatever this is set to.
OBJECTS = {"external_registration_sweep_interval": "1s"}

# why an authority at all: mod/messaging holds no reachability of its own, and
# no handler grants the two mail actions, so a node without one refuses every
# message. Naming a port a driver may serve lets a test admit one; an unserved
# port refuses, which is what a node configured with none answers.
#
# why mod.messaging.read_mailbox_action is listed: no handler grants it, so a
# node without an authority for it refuses every delegated read. A test grants
# a delegated read here.
#
# why mod.messaging.host_mailbox_action is not listed: a node hosts a mailbox
# under the contract the mailbox's identity signs, and auth's chain walk
# answers that without leaving the node.
AUTH_ACTIONS = [
    "mod.messaging.send_action",
    "mod.messaging.receive_action",
    "mod.messaging.read_mailbox_action",
]


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


def _mcp(port: int) -> dict:
    """mod/mcp's config: the server bound, and every declared tool.

    why bound at all: the MCP server is off unless bind_mcp names an endpoint
    (mod/mcp defaults to a single fixed port, which two nodes would fight
    over). An empty bind_mcp turns it off, and mod/mcp still loads.
    """
    tools = [{"name": name,
              "description": f"Put {path} to this node as the agent.",
              "query": f"astral://localnode:{path}"}
             for path, name in NODE_OP_TOOLS.items()]
    tools += [{"name": name,
               "description": f"Put {path} to the peer node as the agent.",
               "query": f"astral://{PEER_ALIAS}:{path}"}
              for path, name in PEER_TOOLS.items()]
    return {"bind_mcp": f"tcp:127.0.0.1:{port}", "tools": tools}


def module_configs(ports: NodePorts, token: str,
                   overrides: dict | None = None) -> dict:
    """module -> the config a node starts with: the harness's base, and a
    world's overrides for the node merged key by key on top.

    why key by key and not the whole module: an override names what a test
    needs changed, and the ports and the token beside it stay the harness's.
    """
    cfg = {
        "apphost": {"listen": [f"tcp:127.0.0.1:{ports.apphost}"],
                    "bind_http": "",
                    # why: the value beside a token is the identity it
                    # authenticates as, and `localnode` is the node itself
                    "tokens": {token: "localnode"}},
        "tcp": {"listen_port": ports.tcp},
        "ether": {"udp_port": ports.ether},
        "kcp": {"listen_port": ports.kcp},
        "mcp": _mcp(ports.mcp),
        "objects": dict(OBJECTS),
        "auth": {"external_authorizers": [
            {"endpoint": ports.authority_url, "actions": list(AUTH_ACTIONS)}]},
    }
    for module, keys in (overrides or {}).items():
        cfg[module] = {**cfg.get(module, {}), **keys}
    return cfg


def serves_mcp(configs: dict) -> bool:
    """Whether a node started with these configs runs its MCP server."""
    return bool(configs.get("mcp", {}).get("bind_mcp"))


def config_text(doc: dict) -> str:
    """A module's config file as astrald reads it.

    why JSON: the harness is stdlib-only and has no YAML writer, and JSON is
    YAML. astrald reads module config with gopkg.in/yaml.v2
    (core/assets LoadYAML), which parses it as it parses block YAML.
    note: every module ignores a LoadYAML error and runs on its defaults, so a
    file it cannot parse reads as a node that ignores the harness.
    why ensure_ascii off: an escaped non-BMP character is a surrogate pair,
    which yaml.v2 refuses ("invalid Unicode character escape code"); YAML
    takes the character itself. NaN and infinity are not JSON at all.
    """
    return json.dumps(doc, indent=2, ensure_ascii=False, allow_nan=False) + "\n"


def render(root: Path, configs: dict) -> None:
    """Write each module's config to <root>/config/<module>.yaml."""
    cfg = Path(root) / "config"
    cfg.mkdir(parents=True, exist_ok=True)
    for module, doc in configs.items():
        (cfg / f"{module}.yaml").write_text(config_text(doc), encoding="utf-8")
