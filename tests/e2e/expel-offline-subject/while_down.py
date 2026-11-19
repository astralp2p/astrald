#!/usr/bin/env python3
"""While node3 is stopped: the User expels node3 on node1.

node1's direct push of the ban to node3 fails, since node3 is down, and node1
keeps retrying it (mod/user/src/push_until_delivered.go). The push to the
local swarm gives node2 the ban; the script waits until node2 lists it, so
node2 holds the ban before node3 comes back.
"""
import asyncio
import time

import astral

from lib.sessionio import load

DELIVER = 30.0     # how long the push to node2 may take
POLL = 0.5


async def banned(c, subject: str) -> bool:
    for e in await c.user.list_expelled():
        if e.expulsion is not None and str(e.expulsion.subject) == subject:
            return True
    return False


async def main():
    doc = load()
    n1, n2 = doc["nodes"]["node1"], doc["nodes"]["node2"]
    id3 = doc["nodes"]["node3"]["identity"]

    async with await astral.connect(n1["endpoint"],
                                    token=doc["facts"]["user_token"]) as c:
        signed = await c.user.expel(id3)
    assert str(signed.expulsion.subject) == id3, (
        f"node1 expelled {signed.expulsion.subject}; want node3 {id3}")

    async with await astral.connect(n2["endpoint"], token=n2["token"]) as c:
        deadline = time.monotonic() + DELIVER
        while not await banned(c, id3):
            assert time.monotonic() < deadline, (
                f"node2 did not list the ban on node3 within {DELIVER:.0f} s")
            await asyncio.sleep(POLL)
    print("while_down: the User expelled node3 on node1 while node3 was down; "
          "node2 holds the ban")


asyncio.run(main())
