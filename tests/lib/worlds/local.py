"""A local world: fresh astrald processes on loopback, for one planned world.

It speaks the world interface (lib/worlds), so a run of several local
worlds and a run on a VM or an attached daemon share one loop.
"""
import asyncio
import shutil
import tempfile
from pathlib import Path

from lib.worlds import World, WorldError, node_entry
from lib.localnode import LocalNode
from lib.nodeconfig import ports_for


class LocalWorld(World):

    def __init__(self, dir: Path, binary: Path, port_base: int,
                 keep: bool = False, config: dict | None = None):
        # why: the nodes write their state under the system temp dir, and dir
        # is a symlink to it until close copies the world back. astrald
        # fsyncs its SQLite database, and on an SD card under write load one
        # fsync stalls for 20-30 s: a node then misses the 30 s readiness wait
        # or a driver's deadline. Where the temp dir is tmpfs, as /tmp is on
        # ant3, no fsync reaches the card.
        self._dir = Path(dir)
        self._live = Path(tempfile.mkdtemp(prefix="astrald-tests-"))
        self._dir.parent.mkdir(parents=True, exist_ok=True)
        self._dir.symlink_to(self._live, target_is_directory=True)
        self.binary = binary
        # why one base for every world of a run: the run holds one port lease,
        # and its worlds run one after another, each from node index 0
        self.port_base = port_base
        # why: a kept world outlives the runner, so its nodes are started
        # without the parent-death signal. Decided before the first node boots.
        self.keep = keep
        # why the world's and not a test's: every test in a world sees the
        # same node, so its config is the union the planner merged for the
        # world, fixed before the first node boots (lib/plan.py)
        self.config = config or {}
        self._nodes = {}

    def ensure(self, test) -> None:
        """Bring the test's roster up; a node already up stays as it is."""
        if test.ops:
            raise WorldError(f"{test.name}: a local world runs no VM "
                             "operations")
        try:
            asyncio.run(self._ensure(test.nodes))
        except Exception as e:
            raise WorldError(f"session: {e}") from e

    async def _ensure(self, names) -> None:
        for name in names:
            if name in self._nodes:
                continue
            node = LocalNode(name, self._dir / name, self.binary,
                             ports_for(self.port_base, len(self._nodes)),
                             self.config.get(name))
            node.start(keep=self.keep)
            self._nodes[name] = node  # track BEFORE readiness — a node that
            await node.wait_ready()   # fails wait_ready must stay stoppable

    def restart(self, names: list) -> None:
        """Stop each named node and boot it again from its own root."""
        try:
            asyncio.run(self._restart(names))
        except Exception as e:
            raise WorldError(f"restart: {e}") from e

    async def _restart(self, names) -> None:
        for name in names:
            node = self._nodes[name]
            node.stop()
            node.start(keep=self.keep)
            await node.wait_ready()
            node.restarts += 1

    def nodes(self) -> dict:
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
            # how many times the world restarted the node, so an oracle can
            # tell a restart happened
            restarts=n.restarts,
        ) for name, n in self._nodes.items()}

    def dead(self) -> list:
        return [name for name, n in self._nodes.items() if not n.alive()]

    def close(self, keep: bool) -> None:
        """Stop the nodes and move their state from the temp dir into dir.

        A kept world stays where it runs, still reached through dir."""
        if keep:
            return
        # why every node, whatever one of them raises: a node left running
        # holds the run's ports, which the next world boots onto
        stuck = []
        try:
            for name, n in self._nodes.items():
                try:
                    n.stop()
                except Exception as e:       # noqa: BLE001
                    stuck.append(f"{name}: {e}")
        finally:
            self._dir.unlink()
            shutil.copytree(self._live, self._dir)
            shutil.rmtree(self._live)
        if stuck:
            raise WorldError(f"close: {'; '.join(stuck)}")
