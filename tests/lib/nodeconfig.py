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
import copy
import fcntl
import json
import re
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
# why the gate and policies: a fresh test node claims without a manager, and
# every admission policy accepts, so tests reach `claimed` with no bootstrap
# mode (the node bootstrap vision's E2E overrides). Omitted keys keep the code
# defaults.
E2E_GATE = {"tokenless": ["apphost.register", "user.info",
                          "user.derive_identity", "user.bootstrap",
                          "user.join"],
            "states": {"fresh": ["*"]}}
E2E_APPHOST_POLICIES = {"app_register": "accept_all"}
E2E_USER = {"policies": {"swarm_join": "accept_all",
                         "swarm_invite": "accept_all"}}


def harness_configs(listen: list, token: str) -> dict:
    """module -> the config every harness node starts with, local or VM:
    apphost on every address in `listen` with `token` as the node's own, the
    E2E gate and policies, and the user policies.

    why one function: a local world and a VM world must start nodes the same
    way, or a test passing locally says nothing about the lab.
    """
    return {
        "apphost": {"listen": list(listen),
                    "bind_http": "",
                    # why: the value beside a token is the identity it
                    # authenticates as, and `localnode` is the node itself
                    "tokens": {token: "localnode"},
                    "gate": copy.deepcopy(E2E_GATE),
                    "policies": dict(E2E_APPHOST_POLICIES)},
        "user": copy.deepcopy(E2E_USER),
    }


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


# the characters a config file escapes although JSON writes them raw: those
# outside YAML's printable set, and the ones YAML reads as a line break
YAML_ESCAPED = re.compile("[\x7f-\x9f\u2028\u2029\ufffe\uffff]")


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
        return authority_url("127.0.0.1", self.authority)


def authority_url(host: str, port: int) -> str:
    """The external authority a node asks, served by a driver at host."""
    return f"http://{host}:{port}/authorize"


# Ports per node: +0 apphost, +1 tcp, +2 ether, +3 kcp, +4 mcp, +5 authority;
# +6..+9 are unused.
PORTS_PER_NODE = 10


@dataclass(frozen=True)
class PortSpan:
    """The ports one world's nodes take: node N's block from base, for at
    most max_nodes nodes.

    why one value: every world that binds or publishes host ports asks the
    same two questions of the run's lease
    """
    base: int
    max_nodes: int

    def ports(self, index: int) -> NodePorts:
        return ports_for(self.base, index)

    def refusal(self, have: int, new: list) -> str | None:
        """Why a world running `have` nodes cannot start `new`, or None."""
        if have + len(new) <= self.max_nodes:
            return None
        return (f"{', '.join(new)}: the world already runs {have} nodes, and "
                f"[ports] span in tests/config.toml gives a world "
                f"{self.max_nodes}")


def overlay(modules: dict, overrides: dict | None) -> dict:
    """modules with each override's keys written over its module's."""
    out = dict(modules)
    for module, keys in (overrides or {}).items():
        out[module] = {**out.get(module, {}), **keys}
    return out


def ports_for(base: int, index: int) -> NodePorts:
    p = base + PORTS_PER_NODE * index
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


# The binds a port must take to read free, by socket kind and address.
#
# why 127.0.0.1 beside 0.0.0.0 for TCP: under SO_REUSEADDR, macOS and the
# BSDs refuse a bind only to a holder of the exact address it names, so a
# 0.0.0.0 probe binds over a 127.0.0.1 listener — the address apphost, mcp
# and the authority hold. Linux refuses it either way. 0.0.0.0 still catches
# a 0.0.0.0 holder, and the dual-stack [::] that Go binds for ":port".
# why UDP binds 0.0.0.0 alone: it sets no SO_REUSEADDR, so every OS refuses
# it beside a holder on any IPv4 address, or a dual-stack [::].
_PROBES = ((socket.SOCK_STREAM, "0.0.0.0"), (socket.SOCK_STREAM, "127.0.0.1"),
           (socket.SOCK_DGRAM, "0.0.0.0"))


def _bindable(port: int) -> bool:
    for kind, addr in _PROBES:
        with socket.socket(socket.AF_INET, kind) as s:
            # why: Go listeners set SO_REUSEADDR, so a port a finished run
            # left in TIME_WAIT is free to astrald and must read free here
            s.setsockopt(socket.SOL_SOCKET, socket.SO_REUSEADDR,
                         kind == socket.SOCK_STREAM)
            try:
                s.bind((addr, port))
            except OSError:
                return False
    return True


def mcp_config(bind: str) -> dict:
    """mod/mcp's config: the server bound at `bind`, and every declared tool.

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
    return {"bind_mcp": bind, "tools": tools}


def auth_config(url: str) -> dict:
    """mod/auth's config: the external authority at `url` for AUTH_ACTIONS."""
    return {"external_authorizers": [
        {"endpoint": url, "actions": list(AUTH_ACTIONS)}]}


def module_configs(ports: NodePorts, token: str,
                   overrides: dict | None = None) -> dict:
    """module -> the config a node starts with: the harness's base, and a
    world's overrides for the node merged key by key on top.

    why key by key and not the whole module: an override names what a test
    needs changed, and the ports and the token beside it stay the harness's.
    """
    cfg = {
        **harness_configs([f"tcp:127.0.0.1:{ports.apphost}"], token),
        "tcp": {"listen_port": ports.tcp},
        "ether": {"udp_port": ports.ether},
        "kcp": {"listen_port": ports.kcp},
        "mcp": mcp_config(f"tcp:127.0.0.1:{ports.mcp}"),
        "objects": dict(OBJECTS),
        "auth": auth_config(ports.authority_url),
    }
    return overlay(cfg, overrides)


def serves_mcp(configs: dict) -> bool:
    """Whether a node started with these configs runs its MCP server."""
    return bool(configs.get("mcp", {}).get("bind_mcp"))


def config_text(doc: dict) -> str:
    """A module's config file as astrald reads it.

    why JSON: the harness is stdlib-only and has no YAML writer, and JSON is
    YAML. astrald reads module config with gopkg.in/yaml.v2
    (core/assets LoadYAML), which parses it as it parses block YAML.
    note: LoadYAML unmarshals strictly, so a file it cannot parse, or one
    that names a key the module does not have, stops the node at startup
    (core/modules.go).
    why ensure_ascii off: an escaped non-BMP character is a surrogate pair,
    which yaml.v2 refuses ("invalid Unicode character escape code"); YAML
    takes the character itself. NaN and infinity are not JSON at all.
    why YAML_ESCAPED is escaped still: yaml.v2 refuses a raw DEL, C1 control,
    U+FFFE or U+FFFF ("control characters are not allowed"), and reads a raw
    NEL, U+2028 or U+2029 as a line break, folding the text around it. JSON's
    \\u escape is YAML's too.
    """
    text = json.dumps(doc, indent=2, ensure_ascii=False, allow_nan=False)
    return YAML_ESCAPED.sub(lambda m: f"\\u{ord(m.group()):04x}", text) + "\n"


def render(root: Path, configs: dict) -> None:
    """Write each module's config to <root>/config/<module>.yaml."""
    cfg = Path(root) / "config"
    cfg.mkdir(parents=True, exist_ok=True)
    for module, doc in configs.items():
        (cfg / f"{module}.yaml").write_text(config_text(doc), encoding="utf-8")
