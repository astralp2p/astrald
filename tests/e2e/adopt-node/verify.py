#!/usr/bin/env python3
"""Oracle: symmetric swarm — port of netsim adopt-node/verify.py assertions.

Both-ends check: same User issued both contracts; each node counts the other
among its linked siblings; node2 holds a live link back to node1.

why membership rather than "the linked sibling": this asked for the FIRST
linked member and compared it to the peer, which is only the same question in
a world with exactly two nodes. The lab now also runs a reflector, and an
operator told to adopt "the other astral node from the local network" quite
reasonably adopted both — so the first linked sibling was the reflector and a
correct swarm read as a wrong one. What this test cares about is that node1
and node2 ended up linked to each other, whoever else is around.
"""
import asyncio
import time

import astral

from lib.sessionio import load


# why a window rather than an instant: #348 syncs the full swarm roster to a
# newly invited node, and that sync is asynchronous. Asking node2 the instant
# adoption returns asks whether the roster is already there, which is a
# different and much weaker question than whether it converges. The window is
# short enough that a roster which never arrives still fails the test.
CONVERGE = 30.0
POLL = 1.0


def linked_siblings(members) -> set:
    """Every sibling this node holds a live link to."""
    return {str(m.identity) for m in members
            if m.linked and m.identity is not None}


async def await_sibling(c, peer: str) -> set:
    """Wait for `peer` to appear among this node's linked siblings."""
    deadline = time.monotonic() + CONVERGE
    sibs = linked_siblings(await c.user.swarm_status())
    while peer not in sibs and time.monotonic() < deadline:
        await asyncio.sleep(POLL)
        sibs = linked_siblings(await c.user.swarm_status())
    return sibs


async def main():
    doc = load()
    n1, n2 = doc["nodes"]["node1"], doc["nodes"]["node2"]
    user_id = doc["facts"]["user_id"]
    user_token = doc["facts"]["user_token"]

    async with await astral.connect(n1["endpoint"], token=user_token) as c:
        i1 = await c.user.info()
        assert str(i1.user_id) == user_id, "node1 contract not issued by User"
        sibs1 = await await_sibling(c, n2["identity"])
        assert n2["identity"] in sibs1, (
            f"node1 has no live link to node2 {n2['identity']}; "
            f"linked siblings: {sorted(sibs1) or 'none'}")

    async with await astral.connect(n2["endpoint"]) as c:      # anonymous
        i2 = await c.user.info()
        assert str(i2.user_id) == user_id, "node2 adopted under a different User"
        sibs2 = await await_sibling(c, n1["identity"])
        assert n1["identity"] in sibs2, (
            f"node2 has no live link to node1 {n1['identity']}; "
            f"linked siblings: {sorted(sibs2) or 'none'} "
            "(symmetric-roster regression)")
        links = await c.nodes.links(experimental=True)
        remotes = {str(l.remote_identity) for l in links
                   if l.remote_identity is not None}
        assert n1["identity"] in remotes, f"no link back to node1 in {remotes}"

    print("oracle: symmetric roster, same issuer, linkback present")


asyncio.run(main())
