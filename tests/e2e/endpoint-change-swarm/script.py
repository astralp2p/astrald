#!/usr/bin/env python3
"""Driver: make the A/B/C swarm: node2 behind NAT, node1, and the reflector admitted.

enter-nat put node2 (A) behind a NAT that admits no inbound connection. The
driver brings the reflector (C) into node1's (B) swarm the way adopt-node
brings node2: each side learns the other's LAN endpoint, node1 links to it as
the User, and the reflector joins through node1 at privileged_member, which
the User confirms on node1 (user.join, user.confirm_join). node2 is given no endpoint of the
reflector: node1 holds the row and node2 pulls it from node1, so node2's
link task for its new sibling has a row to dial. The driver then polls the
three nodes every POLL s until node2 holds a link to node1 and to the
reflector, node1 holds one to the reflector, and each node's swarm lists the
other two, or WAIT s pass.

why node2 is given no endpoint of the reflector: a new member's endpoints reach
a member that cannot dial it by a pull from a sibling that holds them, and node2
reaching the reflector within WAIT is the world check of that. node1 is seeded,
so the world does not tell node1's forwarded notice from a pull from node1; the
unit tests of EP5 cover the forward.

The driver acts and judges nothing. It records the `endpoint_change_swarm`
fact.
"""
import asyncio
import time
from contextlib import AsyncExitStack

import astral

from lib import addrmove, swarmstate
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


async def admit(n: dict, user_token: str) -> dict:
    b, c = n["node1"], n["reflector"]
    try:
        async with await astral.connect(b["endpoint"], token=user_token) as u:
            await u.nodes.new_link(c["identity"], endpoint=c["lan_endpoint"],
                                   strategies="basic", experimental=True, timeout=60.0)
        async with await astral.connect(c["endpoint"], token=c["token"]) as j, \
                await astral.connect(b["endpoint"], token=user_token) as u:
            joined, _ = await swarmstate.admit_member(
                j, u, b["identity"], c["identity"], tier="privileged_member")
        if not joined or joined[0]["Type"] != "mod.auth.signed_contract":
            return {"error": f"user.join answered {joined}"}
        contract = joined[0]["Object"]["Contract"]
        return {"issuer": contract["Issuer"], "subject": contract["Subject"]}
    except Exception as e:  # noqa: BLE001 — every outcome is recorded
        return {"error": f"{type(e).__name__}: {e}"}


async def main():
    doc = load()
    n = doc["nodes"]
    ids = {name: n[name]["identity"] for name in ROLES}
    # node1 and the reflector learn each other's LAN endpoint; node2 learns none by hand
    seeds = (("reflector", "node1"), ("node1", "reflector"))
    for node, peer in seeds:
        async with await addrmove.connect(n[node]) as c:
            await c.nodes.add_endpoint(ids[peer], n[peer]["lan_endpoint"], experimental=True)
    admitted = await admit(n, doc["facts"]["user_token"])
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
    write_facts({"endpoint_change_swarm": {"admitted": admitted, "nodes": snap,
                                           "ready": ready(snap, ids),
                                           "waited": round(time.monotonic() - t0, 1)}})
    print(f"driver: admitted {admitted}; mesh ready={ready(snap, ids)} after "
          f"{time.monotonic() - t0:.1f} s")


asyncio.run(main())
