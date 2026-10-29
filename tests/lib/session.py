"""A session: named astrald nodes on loopback."""
from pathlib import Path

from lib.executors import node_entry
from lib.localnode import LocalNode
from lib.nodeconfig import ports_for


class Session:
    def __init__(self, dir: Path, binary: Path, port_base: int,
                 keep: bool = False):
        self.dir = Path(dir)
        self.dir.mkdir(parents=True, exist_ok=True)
        self.binary = binary
        self.port_base = port_base
        # why: a kept world outlives the runner, so its nodes are started
        # without the parent-death signal. Decided before the first node boots.
        self.keep = keep
        self.nodes = {}

    async def ensure(self, names) -> None:
        for name in names:
            if name in self.nodes:
                continue
            idx = len(self.nodes)
            node = LocalNode(name, self.dir / name, self.binary,
                             ports_for(self.port_base, idx))
            node.start(keep=self.keep)
            self.nodes[name] = node   # track BEFORE readiness — a node that
            await node.wait_ready()   # fails wait_ready must stay stoppable

    async def restart(self, names) -> None:
        """Stop each named node and boot it again from its own root."""
        for name in names:
            node = self.nodes[name]
            node.stop()
            node.start(keep=self.keep)
            await node.wait_ready()
            node.restarts += 1

    def entries(self) -> dict:
        """Every node started so far, as the session view names it."""
        return {name: node_entry(
            endpoint=n.endpoint,
            token=n.token,
            identity=n.identity,
            root=str(n.root),
            tcp_port=n.ports.tcp,
            # why: the address a PEER dials, which is not the address the
            # host dials. On loopback they coincide; in VMs they do not,
            # and a driver that composes 127.0.0.1 from tcp_port is not
            # env-blind however hard the design says it is.
            lan_endpoint=f"tcp:127.0.0.1:{n.ports.tcp}",
            # the port reserved for the node's MCP server, which listens
            # there only when serves_mcp is true
            mcp_url=f"http://127.0.0.1:{n.ports.mcp}",
            serves_mcp=n.serves_mcp,
            # the external authority the node asks about mail; a driver
            # that admits one serves it
            authority_url=n.ports.authority_url,
            # how many times the executor restarted the node, so an
            # oracle can tell a restart happened
            restarts=n.restarts,
        ) for name, n in self.nodes.items()}

    def dead_nodes(self):
        return [name for name, n in self.nodes.items() if not n.alive()]

    def teardown(self) -> None:
        for n in self.nodes.values():
            n.stop()
