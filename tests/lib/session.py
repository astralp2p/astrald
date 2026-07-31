"""A session: named astrald nodes on loopback + accumulated facts."""
import json
from pathlib import Path

from lib.localnode import LocalNode
from lib.nodeconfig import ports_for


class Session:
    def __init__(self, dir: Path, binary: Path, port_base: int):
        self.dir = Path(dir)
        self.dir.mkdir(parents=True, exist_ok=True)
        self.binary = binary
        self.port_base = port_base
        self.nodes = {}
        self.facts = {}

    async def ensure(self, names) -> None:
        for name in names:
            if name in self.nodes:
                continue
            idx = len(self.nodes)
            node = LocalNode(name, self.dir / name, self.binary,
                             ports_for(self.port_base, idx))
            node.start()
            self.nodes[name] = node   # track BEFORE readiness — a node that
            await node.wait_ready()   # fails wait_ready must stay stoppable
        self.write_session_json()

    @property
    def session_json_path(self) -> Path:
        return self.dir / "session.json"

    def write_session_json(self) -> None:
        doc = {"nodes": {}, "facts": self.facts}
        for name, n in self.nodes.items():
            doc["nodes"][name] = {
                "endpoint": n.endpoint,
                "token": n.token,
                "identity": n.identity,
                "root": str(n.root),
                "tcp_port": n.ports.tcp,
                # why: the address a PEER dials, which is not the address the
                # host dials. On loopback they coincide; in VMs they do not,
                # and a driver that composes 127.0.0.1 from tcp_port is not
                # env-blind however hard the design says it is.
                "lan_endpoint": f"tcp:127.0.0.1:{n.ports.tcp}",
            }
        self.session_json_path.write_text(json.dumps(doc, indent=2) + "\n")

    def merge_facts(self, path: Path) -> None:
        if Path(path).exists():
            self.facts.update(json.loads(Path(path).read_text()))
            self.write_session_json()

    def dead_nodes(self):
        return [name for name, n in self.nodes.items() if not n.alive()]

    def teardown(self) -> None:
        for n in self.nodes.values():
            n.stop()
