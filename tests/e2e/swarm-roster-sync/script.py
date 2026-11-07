#!/usr/bin/env python3
"""Driver: node1's User adopts node3 into a swarm that already holds node2.

Only node1 and node3 learn each other's endpoint. node2 is never told where
node3 accepts links, so a link node2 later holds to node3 can only come from
the swarm itself: the roster sync that follows an adoption and the endpoints
it carries.
"""
import asyncio

import astral

from lib.sessionio import load


async def main():
    doc = load()
    n1, n3 = doc["nodes"]["node1"], doc["nodes"]["node3"]
    user_token = doc["facts"]["user_token"]

    # node1 <-> node3 only. nodes.* is Tier-3 gated in astral-py.
    async with await astral.connect(n1["endpoint"], token=n1["token"]) as c:
        await c.nodes.add_endpoint(n3["identity"], n3["lan_endpoint"],
                                   experimental=True)
    async with await astral.connect(n3["endpoint"], token=n3["token"]) as c:
        await c.nodes.add_endpoint(n1["identity"], n1["lan_endpoint"],
                                   experimental=True)

    async with await astral.connect(n1["endpoint"], token=user_token) as c:
        await c.user.adopt(n3["identity"])

    print("driver: node3 adopted by the User on node1; node2 told nothing")


asyncio.run(main())
