#!/usr/bin/env python3
"""Driver: node1, as the User, reads the peer's object over astral.

Port of the netsim read-remote-object flow. The read is issued as the User —
an anonymous query keeps no network zone and would never route to the peer.
"""
import asyncio

import astral

from lib.sessionio import load, write_facts


async def main():
    doc = load()
    n1, n2 = doc["nodes"]["node1"], doc["nodes"]["node2"]
    object_id = doc["facts"]["peer_object_id"]

    async with await astral.connect(n1["endpoint"],
                                    token=doc["facts"]["user_token"]) as c:
        got = await c.objects.load(object_id, target=n2["identity"])

    write_facts({"remote_read_bytes": len(bytes(got))})
    print(f"driver: read {len(bytes(got))} B of {object_id[:16]}… from node2")


asyncio.run(main())
