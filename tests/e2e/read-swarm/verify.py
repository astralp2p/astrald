#!/usr/bin/env python3
"""Oracle: node1 reads through the swarm what node2 holds, and only through it.

Ground truth is node2's own copy, read from its `local` repository. The oracle
repeats each of the driver's three calls itself, then checks the driver
reported the same outcomes: a driver that does nothing must not pass.
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
        held = await c.objects.read(object_id, repo="local")

    async with await astral.connect(n1["endpoint"],
                                    token=facts["user_token"]) as c:
        crossed = await c.objects.read(object_id, zone="dvn")

        try:
            await c.objects.read(object_id, zone="dv")
            local_rejected = False
        except astral.QueryRejected:
            local_rejected = True

        found = [str(i) for i in await c.objects.find(object_id, zone="dvn")]

    assert held, f"node2 holds no bytes for {object_id}"
    assert crossed == held, (
        f"node1 read {crossed!r} through the swarm != node2's {held!r}")
    assert local_rejected, "a dv read on node1, which lacks the object, was served"
    assert n2["identity"] in found, f"objects.find named {found}, not node2"

    assert facts.get("swarm_read_bytes") == len(held), (
        f"the driver read {facts.get('swarm_read_bytes')} B where node2 holds "
        f"{len(held)} B")
    assert facts.get("local_read_rejected") is True, (
        "the driver did not report the dv read rejected")
    assert n2["identity"] in facts.get("swarm_found", []), (
        "the driver's objects.find did not name node2")

    print(f"oracle: node1 read {len(held)} B of {object_id[:16]}… from node2 "
          f"without a target; dv was rejected; find named node2")


asyncio.run(main())
