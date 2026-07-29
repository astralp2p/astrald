"""Render an isolated astrald root's config files (loopback, collision-free).

Every default that two instances would fight over is overridden here:
apphost tcp 8625 / http 8624 / unix socket, tcp listen_port 1791, ether udp
8822. ether gets a distinct port instead of a modules: allowlist — the
allowlist would need every module named and silently rot as astrald grows.
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


@dataclass
class NodePorts:
    apphost: int
    tcp: int
    ether: int


def ports_for(base: int, index: int) -> NodePorts:
    p = base + 10 * index
    return NodePorts(apphost=p, tcp=p + 1, ether=p + 2)


def render(root: Path, ports: NodePorts, token: str) -> None:
    cfg = Path(root) / "config"
    cfg.mkdir(parents=True, exist_ok=True)
    (cfg / "apphost.yaml").write_text(
        APPHOST_YAML.format(apphost=ports.apphost, token=token))
    (cfg / "tcp.yaml").write_text(TCP_YAML.format(tcp=ports.tcp))
    (cfg / "ether.yaml").write_text(ETHER_YAML.format(ether=ports.ether))
