#!/usr/bin/env python3
"""Oracle: adopting node3 on node1 reaches node2, and node3 learns the swarm.

node2 was never told node3's endpoint, so each check below on node2 holds
only if node1's roster sync carried node3's contract and endpoint to its
sibling (astral-docs user-and-swarm.md "Swarm"; astrald
mod/user/src/op_adopt.go PushToLocalSwarm + NewSyncNodesTask):

- node2's swarm_status lists node3 as linked, and node2's nodes.links holds a
  link to node3;
- node3 is claimed by the same User;
- node3's swarm_status lists node1 and node2, both linked.
"""
import asyncio
import time

import astral

from lib.sessionio import load


# why a window rather than an instant: the push and the sync that follow an
# adoption are asynchronous (adopt-node's await_sibling waits the same way).
# Live, node2 linked node3 within 0.5-1 s; 30 s still fails a roster that
# never arrives.
CONVERGE = 30.0
POLL = 1.0


def linked(members) -> set:
    return {str(m.identity) for m in members
            if m.linked and m.identity is not None}


async def until(probe, want):
    """Poll probe() until want(result) or CONVERGE runs out; the last result."""
    deadline = time.monotonic() + CONVERGE
    got = await probe()
    while not want(got) and time.monotonic() < deadline:
        await asyncio.sleep(POLL)
        got = await probe()
    return got


async def main():
    doc = load()
    n1, n2, n3 = (doc["nodes"][n] for n in ("node1", "node2", "node3"))
    user_id = doc["facts"]["user_id"]
    id1, id2, id3 = n1["identity"], n2["identity"], n3["identity"]

    # why node2's own token: the question is what node2 itself believes, and a
    # User session on node1 would only answer for node1.
    async with await astral.connect(n2["endpoint"], token=n2["token"]) as c:
        async def view():
            sibs = linked(await c.user.swarm_status())
            links = {str(l.remote_identity)
                     for l in await c.nodes.links(experimental=True)
                     if l.remote_identity is not None}
            return sibs, links
        sibs2, links2 = await until(
            view, lambda v: id3 in v[0] and id3 in v[1])
    assert id3 in sibs2, (
        f"node2 does not list node3 {id3} as a linked sibling within "
        f"{CONVERGE:.0f} s; linked: {sorted(sibs2) or 'none'} "
        "(roster sync to a sibling regression)")
    assert id3 in links2, (
        f"node2 holds no link to node3 {id3} within {CONVERGE:.0f} s; "
        f"links: {sorted(links2) or 'none'} — node2 was never told node3's "
        "endpoint, so the swarm did not propagate it")

    async with await astral.connect(n3["endpoint"], token=n3["token"]) as c:
        i3 = await c.user.info()
        assert i3.state == "claimed" and str(i3.user_id) == user_id, (
            f"node3 is {i3.state!r} under {i3.user_id!s}, not claimed by the "
            f"User {user_id}")

        async def roster():
            return linked(await c.user.swarm_status())
        sibs3 = await until(roster, lambda s: {id1, id2} <= s)
    assert {id1, id2} <= sibs3, (
        f"node3's linked siblings {sorted(sibs3) or 'none'} lack "
        f"{sorted({id1, id2} - sibs3)} within {CONVERGE:.0f} s "
        "(the invitee never received the swarm roster)")

    print("oracle: node2 linked node3 by roster sync alone; node3 claimed by "
          f"User {user_id[:8]}… and linked to node1 and node2")


asyncio.run(main())
