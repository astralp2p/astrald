#!/usr/bin/env python3
"""Driver (criterion 2.1, step 3 of 3): the User confirms the join, within
the request's hour, and node3 is admitted.

After both restarts, the User, on node1, confirms node3's pending
privileged_member JoinRequest (user.confirm_join) while node3 runs no join:
a confirmation given while the two sides are apart. The user key signs the
membership as issuer and node1 keeps it for the carrier session. node3 then
runs user.join through node1 once more, resending the same request; node1's
carrier hands over the confirmed membership, node3 co-signs it, and node1
stores it with both signatures (astral-docs protocols/user/README.md#joins).

Records `join_resumed`: {joined, request_id, filed_at, timeout, confirmed_at}
— what user.join answered, the request the User confirmed, and when.
"""
import asyncio
import time

import astral

from lib import swarmstate as ss
from lib.sessionio import load, write_facts

TIER = "privileged_member"


async def main():
    doc = load()
    n1, n3 = doc["nodes"]["node1"], doc["nodes"]["node3"]
    async with await astral.connect(n3["endpoint"], token=n3["token"]) as c3, \
            await astral.connect(n1["endpoint"], token=doc["facts"]["user_token"]) as cu:
        req = await ss.find(cu, lambda r: r.type == ss.JOIN and r.names(n3["identity"])
                            and r.status == "pending")
        await ss.confirm_join(cu, req.id)
        confirmed_at = time.time()
        joined = await ss.join(c3, n1["identity"], tier=TIER)
    write_facts({"join_resumed": {"joined": joined, "request_id": req.id,
                                  "filed_at": req.obj.get("FiledAt"),
                                  "timeout": req.obj.get("Timeout"),
                                  "confirmed_at": confirmed_at}})
    print(f"driver: the User confirmed {req.id}; node3's join answered "
          f"{joined[0]['Type'] if joined else 'nothing'}")


asyncio.run(main())
