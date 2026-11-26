#!/usr/bin/env python3
"""Driver (criterion 2.1, step 1 of 3): node3 asks to join while nobody
answers.

node1 and node3 learn each other's endpoint. node3 runs user.join through
node1 (tier member) and gives up after 10 s with nobody answering: the
connection drops, which only pauses the join (astral-docs
protocols/user/ops/user.carry_join.md). The world then restarts node3, the
joiner.

Records `join_unanswered`: {request_id, filed_at, answered} — the JoinRequest
node1 stores, and what user.join answered before the driver gave up.
"""
import asyncio
import time

import astral

from lib import swarmstate as ss
from lib.sessionio import load, write_facts

# why: long enough for the carrier to store the request; nobody answers it
ASK = 10.0


async def main():
    doc = load()
    n1, n3 = doc["nodes"]["node1"], doc["nodes"]["node3"]
    async with await astral.connect(n1["endpoint"], token=n1["token"]) as c:
        await c.nodes.add_endpoint(n3["identity"], n3["lan_endpoint"], experimental=True)
    async with await astral.connect(n3["endpoint"], token=n3["token"]) as c:
        await c.nodes.add_endpoint(n1["identity"], n1["lan_endpoint"], experimental=True)

    answered = None
    async with await astral.connect(n3["endpoint"], token=n3["token"]) as c3:
        try:
            answered = await asyncio.wait_for(ss.join(c3, n1["identity"]), ASK)
        except (asyncio.TimeoutError, TimeoutError):
            pass
    async with await astral.connect(n1["endpoint"], token=doc["facts"]["user_token"]) as cu:
        req = await ss.find(cu, lambda r: r.type == ss.JOIN and r.names(n3["identity"]))
    write_facts({"join_unanswered": {"request_id": req.id, "status": req.status,
                                     "filed_at": req.obj.get("FiledAt"),
                                     "asked_at": time.time(), "answered": answered}})
    print(f"driver: node3's JoinRequest {req.id} waits unanswered on node1")


asyncio.run(main())
