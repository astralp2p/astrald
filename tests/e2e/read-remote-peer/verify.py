#!/usr/bin/env python3
"""Oracle: what node1 reads from the peer is what the peer holds.

Two independent loads: node1 as the User with the query routed to node2, and
node2's own repository, pinned local. Ground truth is the peer's copy, so the
oracle needs no payload file of its own — the object id is a content hash and
the peer is the only holder object-store-peer wrote to.
"""
import asyncio

import astral

from lib.sessionio import load


async def main():
    doc = load()
    n1, n2 = doc["nodes"]["node1"], doc["nodes"]["node2"]
    object_id = doc["facts"]["peer_object_id"]

    async with await astral.connect(n2["endpoint"], token=n2["token"]) as c:
        held = await c.objects.load(object_id, repo="local")

    async with await astral.connect(n1["endpoint"],
                                    token=doc["facts"]["user_token"]) as c:
        crossed = await c.objects.load(object_id, target=n2["identity"])

    assert bytes(held), f"node2 holds no bytes for {object_id}"
    assert bytes(crossed) == bytes(held), (
        f"node1 read {bytes(crossed)!r} != the peer's {bytes(held)!r}")
    print(f"oracle: node1 read {object_id[:16]}… from node2 over astral; "
          f"{len(bytes(held))} B match the peer's own copy")


asyncio.run(main())
