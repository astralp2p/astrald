#!/usr/bin/env python3
"""Oracle (criterion 2.1, step 1 of 3): node3's JoinRequest waits unanswered,
and the joiner's restart leaves it pending.

- user.join answered nothing while nobody answered;
- after node3's restart, node1 still lists node3's one JoinRequest, pending;
- node3 is not claimed.
"""
import asyncio

import astral

from lib import swarmstate as ss
from lib.sessionio import load


async def main():
    doc = load()
    n1, n3 = doc["nodes"]["node1"], doc["nodes"]["node3"]
    f = doc["facts"]["join_unanswered"]
    assert f["answered"] is None, f"user.join answered while nobody answered: {f['answered']}"
    async with await astral.connect(n1["endpoint"], token=doc["facts"]["user_token"]) as cu:
        joins = [r for r in await ss.requests(cu, all=True)
                 if r.type == ss.JOIN and r.names(n3["identity"])]
    assert [(r.id, r.status) for r in joins] == [(f["request_id"], "pending")], (
        f"node1 lists node3's join requests as {[(r.id, r.status) for r in joins]}")
    async with await astral.connect(n3["endpoint"], token=n3["token"]) as c3:
        info = await c3.user.info()
    assert info.user_id is None or str(info.user_id) != doc["facts"]["user_id"], (
        "node3 is claimed by the User before any answer")
    print("oracle: node3's JoinRequest waits pending on node1 after the joiner's restart")


asyncio.run(main())
