#!/usr/bin/env python3
"""Oracle: symmetric swarm — port of netsim adopt-node/verify.py assertions.

Both-ends check: same User issued both contracts; each node's linked sibling
is the other; node2 holds a live link back to node1.
"""
import asyncio

import astral

from lib.sessionio import load


def linked_sibling(members):
    for m in members:
        if m.linked and m.identity is not None:
            return str(m.identity)
    return None


async def main():
    doc = load()
    n1, n2 = doc["nodes"]["node1"], doc["nodes"]["node2"]
    user_id = doc["facts"]["user_id"]
    user_token = doc["facts"]["user_token"]

    async with await astral.connect(n1["endpoint"], token=user_token) as c:
        i1 = await c.user.info()
        assert str(i1.user_id) == user_id, "node1 contract not issued by User"
        sib1 = linked_sibling(await c.user.swarm_status())
        assert sib1 == n2["identity"], (
            f"node1 linked sibling {sib1} != node2 {n2['identity']}")

    async with await astral.connect(n2["endpoint"]) as c:      # anonymous
        i2 = await c.user.info()
        assert str(i2.user_id) == user_id, "node2 adopted under a different User"
        sib2 = linked_sibling(await c.user.swarm_status())
        assert sib2 == n1["identity"], (
            f"node2 linked sibling {sib2} != node1 {n1['identity']} "
            "(symmetric-roster regression)")
        links = await c.nodes.links(experimental=True)
        remotes = {str(l.remote_identity) for l in links
                   if l.remote_identity is not None}
        assert n1["identity"] in remotes, f"no link back to node1 in {remotes}"

    print("oracle: symmetric roster, same issuer, linkback present")


asyncio.run(main())
