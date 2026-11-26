#!/usr/bin/env python3
"""Oracle (criterion 2.1, step 2 of 3): the restarted joiner resent the same
JoinRequest, and the carrier's restart keeps it pending.

- after node3's resend, node1 listed exactly one JoinRequest of node3: the
  one step 1 recorded, pending — the joiner kept it across its restart and
  sent the same object again (astral-docs protocols/user/ops/user.join.md);
- after node1's restart, node1 still lists that one request, pending.
"""
import asyncio

import astral

from lib import swarmstate as ss
from lib.sessionio import load


async def main():
    doc = load()
    n1, n3 = doc["nodes"]["node1"], doc["nodes"]["node3"]
    first = doc["facts"]["join_unanswered"]["request_id"]
    f = doc["facts"]["join_resent"]
    assert f["answered"] is None, f"user.join answered while nobody answered: {f['answered']}"
    assert f["joins"] == [[first, "pending"]], (
        f"after the joiner's restart node1 listed {f['joins']}; want the one request {first}")
    async with await astral.connect(n1["endpoint"], token=doc["facts"]["user_token"]) as cu:
        joins = [[r.id, r.status] for r in await ss.requests(cu, all=True)
                 if r.type == ss.JOIN and r.names(n3["identity"])]
    assert joins == [[first, "pending"]], f"after the carrier's restart node1 lists {joins}"
    print("oracle: one JoinRequest of node3, the first one, waits pending through both restarts")


asyncio.run(main())
