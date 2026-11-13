"""An attach world: no spawning at all — the daemon already up is the world.

The code-and-debug loop. Point a test at the astrald you are hacking on and
let the standard oracle judge it, with open eyes: nothing here is hermetic,
nothing is torn down, and a destroying test really changes your daemon.

The planner holds attach's guards (lib/plan.py, rule 8): one world, one
roster node, no prereq to build, no VM operation, nothing local only, no
restart. A test that serves a listener on the test host (host_listener)
plans here, and the runner fails its prepare when the daemon is remote.
"""
import asyncio
import ipaddress

import astral
from astral.client import resolve_endpoint, resolve_token

from lib.worlds import World, WorldError, node_entry


def host_ip(endpoint: str) -> str | None:
    """127.0.0.1 when the daemon at endpoint runs on the test host — a unix
    socket or a loopback TCP address — else None: a remote daemon cannot
    dial a listener the test serves here."""
    proto, _, addr = endpoint.partition(":")
    if proto == "unix":
        return "127.0.0.1"
    if proto != "tcp":
        return None
    host = addr.rpartition(":")[0].strip("[]")
    try:
        loopback = host == "localhost" or ipaddress.ip_address(host).is_loopback
    except ValueError:
        return None
    return "127.0.0.1" if loopback else None


class AttachWorld(World):
    def __init__(self):
        self._node = None

    def ensure(self, test) -> None:
        """Probe the ambient daemon once, under the test's one roster name."""
        if self._node is None:
            self._node = asyncio.run(self._probe(test.nodes[0]))

    async def _probe(self, name: str) -> dict:
        endpoint, token = resolve_endpoint(), resolve_token()
        try:
            async with await astral.connect(endpoint, token=token) as c:
                identity = str(await c.apphost.whoami()) if c.authenticated \
                    else ""
                if not await c.shell.spec():
                    raise WorldError(f"{endpoint}: empty op catalog")
        except astral.AstralError as e:
            raise WorldError(f"{endpoint}: {e}") from e
        return {"name": name, "endpoint": endpoint, "token": token or "",
                "identity": identity}

    def nodes(self) -> dict:
        if self._node is None:
            return {}
        n = self._node
        # note: the daemon's ports and roots are its owner's, not the run's,
        # so attach knows only how to reach it
        return {n["name"]: node_entry(
            endpoint=n["endpoint"], token=n["token"], identity=n["identity"],
            host_ip=host_ip(n["endpoint"]))}

    def dead(self) -> list:
        return []

    def close(self, keep: bool) -> None:
        """Never stops the daemon. It was running before this run and stays."""
