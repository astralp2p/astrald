#!/usr/bin/env python3
"""Driver (criterion 2.1, step 3 of 3): the User says yes, within the
request's hour, and node3 is admitted.

After both restarts, the User, on node1, approves node3's pending
JoinRequest (user.answer_request) while node3 runs no join: a yes given while
the two sides are apart. node3 then runs user.join through node1 once more,
resending the same request, and node1 hands it the membership.

Records `join_resumed`: {joined, request_id, approved_at} — what user.join
answered, the request the User approved, and when.
"""
import asyncio
import time

import astral

from lib import swarmstate as ss
from lib.sessionio import load, write_facts


async def main():
    doc = load()
    n1, n3 = doc["nodes"]["node1"], doc["nodes"]["node3"]
    async with await astral.connect(n3["endpoint"], token=n3["token"]) as c3, \
            await astral.connect(n1["endpoint"], token=doc["facts"]["user_token"]) as cu:
        req = await ss.find(cu, lambda r: r.type == ss.JOIN and r.names(n3["identity"])
                            and r.status == "pending")
        await ss.answer(cu, req.id, "approve")
        approved_at = time.time()
        joined = await ss.join(c3, n1["identity"])
    write_facts({"join_resumed": {"joined": joined, "request_id": req.id,
                                  "filed_at": req.obj.get("FiledAt"),
                                  "timeout": req.obj.get("Timeout"),
                                  "approved_at": approved_at}})
    print(f"driver: the User approved {req.id}; node3's join answered "
          f"{joined[0]['Type'] if joined else 'nothing'}")


asyncio.run(main())
