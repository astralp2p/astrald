"""Render an isolated astrald root's config files (loopback, collision-free).

Every default that two instances would fight over is overridden here:
apphost tcp 8625 / http 8624 / unix socket, tcp listen_port 1791, ether udp
8822, kcp listen_port 1792. ether/kcp get distinct ports instead of a
modules: allowlist — the allowlist would need every module named and
silently rot as astrald grows.
"""
from dataclasses import dataclass
from pathlib import Path

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


def render(root: Path, ports: NodePorts, token: str) -> None:
    cfg = Path(root) / "config"
    cfg.mkdir(parents=True, exist_ok=True)
    (cfg / "apphost.yaml").write_text(
        APPHOST_YAML.format(apphost=ports.apphost, token=token))
    (cfg / "tcp.yaml").write_text(TCP_YAML.format(tcp=ports.tcp))
    (cfg / "ether.yaml").write_text(ETHER_YAML.format(ether=ports.ether))
    (cfg / "kcp.yaml").write_text(KCP_YAML.format(kcp=ports.kcp))
    (cfg / "mcp.yaml").write_text(MCP_YAML.format(mcp=ports.mcp))
