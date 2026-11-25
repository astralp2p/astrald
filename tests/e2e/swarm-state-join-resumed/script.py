#!/usr/bin/env python3
"""Driver (criterion 2.1, step 3 of 3): the User says yes, within the
request's hour, and node3 is admitted.

After both restarts, node3 runs user.join through node1 once more while the
User, on node1, approves node3's pending JoinRequest (user.answer_request).

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
        joined, req = await ss.admit_member(c3, cu, n1["identity"], n3["identity"])
    write_facts({"join_resumed": {"joined": joined, "request_id": req.id,
                                  "filed_at": req.obj.get("FiledAt"),
                                  "timeout": req.obj.get("Timeout"),
                                  "approved_at": time.time()}})
    print(f"driver: the User approved {req.id}; node3's join answered "
          f"{joined[0]['Type'] if joined else 'nothing'}")


asyncio.run(main())
