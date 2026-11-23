#!/usr/bin/env python3
"""Oracle: the swarm, not the driver, links every pair of four siblings, and
every roster lists the same four members.

- every node lists a link to each of the other three within 30 s of node4's
  adoption (derived: a pushed membership contract makes the node pull the
  subject's endpoints, and a first-hand pull forwards a notice one hop, each
  pull bounded to 20 s, astral-docs protocols/nodes/README.md:54-90; the same
  30 s bounds the mesh in tests/e2e/relay-contract-race/driver/main.go:32);
- every node's `user.swarm_status` lists exactly the four identities, itself
  included (protocols/user/ops/user.swarm_status.md: one member per node of
  the swarm; topics/user-and-swarm.md:406-417 shows the new node among them;
  astrald mod/user/src/contracts.go:157-185 lists every membership contract
  of the User, the node's own among them), read live at the oracle and not
  held to the 30 s mesh bound: no source bounds roster sync;
- the oracle reads the links again live: the world it leaves to F1-F4 and
  M14 holds them still.
"""
import asyncio
import time
from contextlib import AsyncExitStack

import astral

from lib import linkview
from lib.sessionio import load

NAMES = ["node1", "node2", "node3", "node4"]
MESH = 30.0
# why: a live read right after the driver may meet a link the node is
# replacing; the driver already saw the mesh, so a short wait suffices
LIVE = 10.0


def unlinked(links: dict, ids: dict) -> list:
    """Every "a->b" for which node a lists no link to node b."""
    return [f"{a}->{b}" for a in NAMES for b in NAMES
            if a != b and not linkview.to(links.get(a) or [], ids[b])]


def wrong_rosters(rosters: dict, ids: dict) -> dict:
    """{node: its roster's identities} for every roster that is not the four."""
    want = set(ids.values())
    return {n: sorted(m["identity"] for m in rosters.get(n) or [])
            for n in NAMES
            if {m["identity"] for m in rosters.get(n) or []} != want}


async def live(clients: dict) -> dict:
    out = {"links": {}, "rosters": {}}
    for n, c in clients.items():
        out["links"][n] = await linkview.links(c)
        out["rosters"][n] = [{"identity": str(m.identity), "linked": bool(m.linked)}
                             for m in await c.user.swarm_status() if m.identity is not None]
    return out


async def main():
    doc = load()
    nodes, f = doc["nodes"], doc["facts"]["four_siblings"]
    ids = {n: nodes[n]["identity"] for n in NAMES}

    assert f.get("mesh_at") is not None, (
        f"not every pair linked within {MESH:.0f} s of node4's adoption; missing "
        f"{unlinked(f.get('links') or {}, ids)} (the swarm did not carry node3's and "
        "node4's endpoints to every member)")
    took = f["mesh_at"] - f["adopted_at"]
    assert took <= MESH, f"every pair linked {took:.1f} s after node4's adoption; want ≤ {MESH:.0f} s"
    # why the rosters are judged live only: no source bounds roster sync, and
    # the driver stops polling at the mesh bound

    async with AsyncExitStack() as stack:
        clients = {n: await stack.enter_async_context(
            await astral.connect(nodes[n]["endpoint"], token=nodes[n]["token"]))
            for n in NAMES}
        deadline = time.monotonic() + LIVE
        now = await live(clients)
        while (unlinked(now["links"], ids) or wrong_rosters(now["rosters"], ids)) \
                and time.monotonic() < deadline:
            await asyncio.sleep(0.5)
            now = await live(clients)
    assert not unlinked(now["links"], ids), (
        f"at the oracle, unlinked pairs {unlinked(now['links'], ids)}")
    assert not wrong_rosters(now["rosters"], ids), (
        f"at the oracle, rosters that are not the four siblings "
        f"{sorted(ids.values())}: {wrong_rosters(now['rosters'], ids)}; at the "
        f"driver's last poll {wrong_rosters(f.get('rosters') or {}, ids)}, errors "
        f"{f.get('roster_errors') or {}}")

    print(f"oracle: every pair of the four siblings linked {took:.1f} s after node4's "
          "adoption; every roster lists the four")


asyncio.run(main())
