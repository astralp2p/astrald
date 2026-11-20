#!/usr/bin/env python3
"""Driver: make the A/B/C swarm: node2 behind NAT, node1, and the reflector adopted.

enter-nat put node2 (A) behind a NAT that admits no inbound connection. The
driver adopts the reflector (C) into node1's (B) swarm as the User, the way
adopt-node adopts node2: each side learns the other's LAN endpoint, node1
links to it, and the User adopts it. node2 learns the reflector's LAN
endpoint too, so its link task for its new sibling has a row to dial. The
driver then polls the three nodes every POLL s until node2 holds a link to
node1 and to the reflector, node1 holds one to the reflector, and each
node's swarm lists the other two, or WAIT s pass.

why the reflector's endpoint on node2 is given: this test builds the world
endpoint-change-relink starts from. How a new member's endpoints reach a
member that cannot dial it is the A3G14 gap, which this ticket leaves open.

The driver acts and judges nothing. It records the `endpoint_change_swarm`
fact.
"""
import asyncio
import time
from contextlib import AsyncExitStack

import astral

from lib import addrmove
from lib.sessionio import load, write_facts

POLL = 1.0
WAIT = 180.0
ROLES = {"node1": "B", "node2": "A", "reflector": "C"}


async def roster(c) -> list:
    return sorted(str(m.identity) for m in await c.user.swarm_status())


async def snapshot(clients: dict) -> dict:
    out = {}
    for name, c in clients.items():
        out[name] = {"links": await addrmove.ask(addrmove.links(c), []),
                     "swarm": await addrmove.ask(roster(c), [])}
    return out


def ready(snap: dict, ids: dict) -> bool:
    def linked(a, b):
        return bool(addrmove.to(snap[a]["links"], ids[b]))
    others = {n: set(ids.values()) - {ids[n]} for n in ids}
    return (linked("node2", "node1") and linked("node2", "reflector")
            and linked("node1", "reflector")
            and all(others[n] <= set(snap[n]["swarm"]) for n in ids))


async def adopt(n: dict, user_token: str) -> dict:
    b, c = n["node1"], n["reflector"]
    try:
        async with await astral.connect(b["endpoint"], token=user_token) as u:
            await u.nodes.new_link(c["identity"], endpoint=c["lan_endpoint"],
                                   strategies="basic", experimental=True, timeout=60.0)
            signed = await u.user.adopt(c["identity"])
            return {"issuer": str(signed.contract.issuer), "subject": str(signed.contract.subject)}
    except Exception as e:  # noqa: BLE001 — every outcome is recorded
        return {"error": f"{type(e).__name__}: {e}"}


async def main():
    doc = load()
    n = doc["nodes"]
    ids = {name: n[name]["identity"] for name in ROLES}
    # each side learns the other's LAN endpoint; node2 dials, so it learns the reflector's
    seeds = (("reflector", "node1"), ("node1", "reflector"), ("node2", "reflector"))
    for node, peer in seeds:
        async with await addrmove.connect(n[node]) as c:
            await c.nodes.add_endpoint(ids[peer], n[peer]["lan_endpoint"], experimental=True)
    adopted = await adopt(n, doc["facts"]["user_token"])
    for node in ROLES:
        async with await addrmove.connect(n[node]) as c:
            for peer in ROLES:
                await c.dir.set_alias(ids[peer], peer)

    t0 = time.monotonic()
    async with AsyncExitStack() as stack:
        clients = {name: await stack.enter_async_context(await addrmove.connect(n[name]))
                   for name in ROLES}
        snap = await snapshot(clients)
        while not ready(snap, ids) and time.monotonic() - t0 < WAIT:
            await asyncio.sleep(POLL)
            snap = await snapshot(clients)
    write_facts({"endpoint_change_swarm": {"adopted": adopted, "nodes": snap,
                                           "ready": ready(snap, ids),
                                           "waited": round(time.monotonic() - t0, 1)}})
    print(f"driver: adopted {adopted}; mesh ready={ready(snap, ids)} after "
          f"{time.monotonic() - t0:.1f} s")


asyncio.run(main())
