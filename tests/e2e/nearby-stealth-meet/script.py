#!/usr/bin/env python3
"""Driver: claim the reflector with the User's words, then wait for it and node1 to find each other.

The driver acts and judges nothing. It records each node's swarm and claims
the still unowned reflector with the User's words through a manager, as
introduce-node claims `replacement`. The reflector then holds its own contract
alone, and node1 and node2 hold none of it. While each node's log is
recorded, the driver waits, with no scan, no endpoint, no alias and no
introduction, until node1 and the reflector list each other linked and node2
and the reflector list each other.

why no wait before the claim: node1 and node2 hold the reflector, unowned, as
a verified visible node under its IP. When it comes back as the User's, its
user changes, and nearby emits PeerSeen for a node whose user changed
(mod/nearby/src/cache.go storeVerified), so both act on it. The reflector,
once claimed, recognises node1 and node2 from their stealth hints. Contact can
therefore start from either side, and the log records which.
"""
import asyncio
import time

import astral
from astral.errors import AstralError

from lib import jsonops, nearby
from lib.nodelog import LogTap
from lib.sessionio import load, write_facts

MANAGER = {"Type": "mod.sys.register_manager_request", "Object": {"Name": "manager"}}
# why: after the claim the reflector recognises node1 and node2 at their next
# status, at most 95 s away, then links, reads their contract and introduces
# itself; the sync and the sibling links follow
CONVERGE = 240.0
POLL = 2.0
# the user module's nearby contact lines (mod/user/src/peer_seen.go)
NEEDLES = ("nearby peer", "is nearby at")


async def members(node: dict, token: str | None = None) -> dict | None:
    """identity -> linked, as node's swarm lists it; None without a swarm."""
    try:
        async with await astral.connect(node["endpoint"], token=token or node["token"]) as c:
            return {str(m.identity): bool(m.linked) for m in await c.user.swarm_status()
                    if m.identity is not None}
    except AstralError:
        return None


async def listing(node: dict) -> list:
    async with await astral.connect(node["endpoint"], token=node["token"]) as c:
        return await nearby.statuses(c)


async def claim(node: dict, words: str) -> dict:
    """Appoint a manager on node and claim it with words as the manager."""
    async with await astral.connect(node["endpoint"], token=node["token"]) as c:
        manager = await jsonops.value(c, "sys.register_manager?in=json&out=json", MANAGER)
    async with await astral.connect(node["endpoint"], token=manager["Token"]) as m:
        return await jsonops.value(m, "user.bootstrap?in=json&out=json",
                                   {"Type": "string16", "Object": words})


def met(v: dict, ids: dict) -> bool:
    n1, n2, r = ids["node1"], ids["node2"], ids["reflector"]
    return (v["node1"] is not None and v["node1"].get(r) is True
            and v["reflector"] is not None and v["reflector"].get(n1) is True
            and v["node2"] is not None and r in v["node2"]
            and n2 in v["reflector"])


async def main():
    doc = load()
    nodes, user = doc["nodes"], doc["facts"]["nearby_user"]
    ids = {k: nodes[k]["identity"] for k in ("node1", "node2", "reflector")}
    n1, n2, r = nodes["node1"], nodes["node2"], nodes["reflector"]

    before = {"node1": await members(n1), "node2": await members(n2)}

    async with LogTap(n1) as t1, LogTap(n2) as t2, LogTap(r) as tr:
        claimed = await claim(r, user["words"])
        before["reflector"] = await members(r)

        t0 = time.monotonic()
        while True:
            views = {k: await members(nodes[k]) for k in ids}
            if met(views, ids) or time.monotonic() - t0 >= CONVERGE:
                break
            await asyncio.sleep(POLL)
        converged = round(time.monotonic() - t0, 1)

        entries = {
            "node1_of_reflector": nearby.of(await listing(n1), ids["reflector"]),
            "node2_of_reflector": nearby.of(await listing(n2), ids["reflector"]),
            "reflector_of_node1": nearby.of(await listing(r), ids["node1"]),
        }
        modes = {}
        for k in ids:
            async with await astral.connect(nodes[k]["endpoint"], token=nodes[k]["token"]) as c:
                modes[k] = await nearby.stored_mode(c)
        taps = {"node1": t1, "node2": t2, "reflector": tr}

    write_facts({"nearby_stealth": {
        "user": user["identity"], "claimed": claimed["Identity"],
        "before": before, "after": views, "converged": converged,
        "met": met(views, ids), "entries": entries, "modes": modes,
        "log": {k: t.find(*NEEDLES) for k, t in taps.items()},
        "taps": {k: t.state() for k, t in taps.items()},
    }})
    print(f"driver: reflector claimed for {claimed['Identity'][:16]}…; "
          f"met={met(views, ids)} after {converged:.0f}s")


asyncio.run(main())
