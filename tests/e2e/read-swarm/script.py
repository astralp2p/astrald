#!/usr/bin/env python3
"""Driver: node1, as the User, reads node2's object without naming node2.

The read names no target and no repository: the finders on node1 locate the
object, and node1 reads it from node2. A `dv` read stays on node1 and is
rejected, because node1 does not hold the object.
"""
import asyncio

import astral

from lib.sessionio import load, write_facts


async def main():
    doc = load()
    n1 = doc["nodes"]["node1"]
    object_id = doc["facts"]["peer_object_id"]

    async with await astral.connect(n1["endpoint"],
                                    token=doc["facts"]["user_token"]) as c:
        got = await c.objects.read(object_id, zone="dvn")

        try:
            await c.objects.read(object_id, zone="dv")
            local_rejected = False
        except astral.QueryRejected:
            local_rejected = True

        found = [str(i) for i in await c.objects.find(object_id, zone="dvn")]

    write_facts({
        "swarm_read_bytes": len(got),
        "local_read_rejected": local_rejected,
        "swarm_found": found,
    })
    print(f"driver: read {len(got)} B of {object_id[:16]}… through the swarm; "
          f"dv rejected={local_rejected}; find named {len(found)}")


asyncio.run(main())
