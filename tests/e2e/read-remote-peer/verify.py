#!/usr/bin/env python3
"""Oracle: what node1 reads from the peer is what the peer holds.

Two independent loads: node1 as the User with the query routed to node2, and
node2's own repository, pinned local. Ground truth is the peer's copy, so the
oracle needs no payload file of its own — the object id is a content hash and
the peer is the only holder object-store-peer wrote to.

why the driver's own count is asserted too: this flow leaves nothing behind. It
reads an object that object-store-peer already placed on node2, and a read
changes no state, so every claim above is one the oracle establishes by itself.
Without the last assertion a driver that does nothing at all passes here —
measured, not supposed: with script.py reduced to a print, this test reported
PASS. Under --driver agent that is an operator credited for a flow it never
performed, which is the one thing the script/agent split exists to detect.
"""
import asyncio

import astral

from lib.sessionio import load


async def main():
    doc = load()
    n1, n2 = doc["nodes"]["node1"], doc["nodes"]["node2"]
    facts = doc["facts"]
    object_id = facts["peer_object_id"]

    async with await astral.connect(n2["endpoint"], token=n2["token"]) as c:
        held = await c.objects.load(object_id, repo="local")

    async with await astral.connect(n1["endpoint"],
                                    token=doc["facts"]["user_token"]) as c:
        crossed = await c.objects.load(object_id, target=n2["identity"])

    assert bytes(held), f"node2 holds no bytes for {object_id}"
    assert bytes(crossed) == bytes(held), (
        f"node1 read {bytes(crossed)!r} != the peer's {bytes(held)!r}")

    # The driver's own result, which is the only part of this test the driver
    # can fail. Everything above the oracle establishes by itself.
    assert "remote_read_bytes" in facts, (
        "the driver reported no remote_read_bytes — it never performed the "
        "read, and the loads above are the oracle's own work")
    assert facts["remote_read_bytes"] == len(bytes(held)), (
        f"the driver read {facts['remote_read_bytes']} B where the peer holds "
        f"{len(bytes(held))} B — the flow did not read this object")

    print(f"oracle: node1 read {object_id[:16]}… from node2 over astral; "
          f"{len(bytes(held))} B match the peer's own copy, and the driver "
          f"reported {facts['remote_read_bytes']} B")


asyncio.run(main())
