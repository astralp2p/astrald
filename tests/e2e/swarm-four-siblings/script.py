#!/usr/bin/env python3
"""Driver: node1's User adopts node3, then node4, into the swarm of node1 and
node2, and the driver waits until the swarm links every pair.

Only node1 learns node3's and node4's endpoints, and node3 and node4 learn
only node1's. node2 learns nothing from the driver, and node3 and node4 learn
nothing of each other, so a link between node2 and a new node, or between
node3 and node4, comes from the swarm alone.

The driver polls each node's links and `user.swarm_status` every 0.5 s, up to
30 s after node4's adoption. It judges nothing and records the
`four_siblings` fact: {adopted_node3_at, adopted_at (node4's), mesh_at (the
first poll at which every node linked the other three, null when none did),
links, rosters, roster_errors}, the last three as of the last poll.
"""
import asyncio
import time
from contextlib import AsyncExitStack

import astral

from lib import linkview
from lib.sessionio import load, write_facts

NAMES = ["node1", "node2", "node3", "node4"]
POLL = 0.5
# why: the oracle's bound; polling longer would only hide a slow mesh
MESH = 30.0


async def meet(c1, n1: dict, clients: dict, nodes: dict) -> None:
    """node1 and each new node learn each other's endpoint; nobody else."""
    for name in ("node3", "node4"):
        peer = nodes[name]
        await c1.nodes.add_endpoint(peer["identity"], peer["lan_endpoint"],
                                    experimental=True)
        await clients[name].nodes.add_endpoint(n1["identity"], n1["lan_endpoint"],
                                               experimental=True)


async def roster(c) -> tuple[list, str | None]:
    """The node's swarm members, sorted, and the error that kept them.

    why an error is a value: a new node answers `no active contract` until
    its claim lands (astral-docs topics/user-and-swarm.md, Joining a swarm).
    """
    try:
        members = await c.user.swarm_status()
    except Exception as e:  # noqa: BLE001 — the answer is recorded, not judged
        return [], f"{type(e).__name__}: {e}"
    return sorted(({"identity": str(m.identity), "linked": bool(m.linked)}
                   for m in members if m.identity is not None),
                  key=lambda m: m["identity"]), None


async def view(clients: dict) -> dict:
    """Each node's links, as rows, its swarm roster and the roster's error."""
    out = {"links": {}, "rosters": {}, "roster_errors": {}}
    for name, c in clients.items():
        out["links"][name] = await linkview.links(c)
        out["rosters"][name], err = await roster(c)
        if err:
            out["roster_errors"][name] = err
    return out


def meshed(links: dict, ids: dict) -> bool:
    """Whether every node lists a link to each of the other three."""
    return all(linkview.to(links[a], ids[b])
               for a in NAMES for b in NAMES if a != b)


def full(rosters: dict, ids: dict) -> bool:
    """Whether every roster lists exactly the four identities."""
    want = set(ids.values())
    return all({m["identity"] for m in r} == want for r in rosters.values())


async def main():
    doc = load()
    nodes = doc["nodes"]
    ids = {n: nodes[n]["identity"] for n in NAMES}
    facts = {}
    async with AsyncExitStack() as stack:
        clients = {n: await stack.enter_async_context(
            await astral.connect(nodes[n]["endpoint"], token=nodes[n]["token"]))
            for n in NAMES}
        cu = await stack.enter_async_context(
            await astral.connect(nodes["node1"]["endpoint"], token=doc["facts"]["user_token"]))
        await meet(clients["node1"], nodes["node1"], clients, nodes)

        await cu.user.adopt(ids["node3"])
        facts["adopted_node3_at"] = time.time()
        await cu.user.adopt(ids["node4"])
        facts["adopted_at"] = time.time()

        facts["mesh_at"] = None
        deadline = facts["adopted_at"] + MESH
        while True:
            seen = await view(clients)
            if facts["mesh_at"] is None and meshed(seen["links"], ids):
                facts["mesh_at"] = time.time()
            if (facts["mesh_at"] is not None and full(seen["rosters"], ids)) \
                    or time.time() >= deadline:
                break
            await asyncio.sleep(POLL)
        facts.update(seen)

    write_facts({"four_siblings": facts})
    took = (f"{facts['mesh_at'] - facts['adopted_at']:.1f} s"
            if facts["mesh_at"] is not None else f"not within {MESH:.0f} s")
    print(f"driver: node3 and node4 adopted on node1; every pair linked {took} "
          "after node4's adoption")


asyncio.run(main())
