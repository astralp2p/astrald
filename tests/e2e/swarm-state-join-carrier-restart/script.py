#!/usr/bin/env python3
"""Driver (criterion 2.1, step 2 of 3): the restarted joiner asks again, and
the carrier restarts.

node3, restarted by step 1, runs user.join through node1 again (tier member)
and gives up after 10 s with nobody answering. The world then restarts node1,
the carrier.

Records `join_resent`: {joins} — node3's JoinRequests node1 lists after the
resend, as [entry ID, status] pairs.
"""
import asyncio

import astral

from lib import swarmstate as ss
from lib.sessionio import load, write_facts

ASK = 10.0


async def main():
    doc = load()
    n1, n3 = doc["nodes"]["node1"], doc["nodes"]["node3"]
    answered = None
    async with await astral.connect(n3["endpoint"], token=n3["token"]) as c3:
        try:
            answered = await asyncio.wait_for(ss.join(c3, n1["identity"]), ASK)
        except (asyncio.TimeoutError, TimeoutError):
            pass
    async with await astral.connect(n1["endpoint"], token=doc["facts"]["user_token"]) as cu:
        joins = [[r.id, r.status] for r in await ss.requests(cu, all=True)
                 if r.type == ss.JOIN and r.names(n3["identity"])]
    write_facts({"join_resent": {"joins": joins, "answered": answered}})
    print(f"driver: node3 asked again through node1; node1 lists {joins}")


asyncio.run(main())
