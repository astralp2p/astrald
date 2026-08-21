#!/usr/bin/env python3
"""Oracle: acting as the User works and an active contract exists.

Ported from netsim bootstrap-user-software-key/verify.sh: whoami must equal
the user id; user.info succeeding IS the proof of an active contract (it
rejects with code 2 when none is active).
"""
import asyncio

import astral

from lib.sessionio import load


async def main():
    doc = load()
    n1 = doc["nodes"]["node1"]
    user_id = doc["facts"]["user_id"]
    user_token = doc["facts"]["user_token"]
    assert user_id and user_token, "bootstrap facts missing"

    async with await astral.connect(n1["endpoint"], token=user_token) as c:
        who = await c.apphost.whoami()
        assert str(who) == user_id, f"whoami {who} != user {user_id}"
        info = await c.user.info()          # raises if no active contract
        assert str(info.user_id) == user_id, (
            f"contract issuer {info.user_id} != user {user_id}")
        assert str(info.node_id) == n1["identity"], (
            f"contract subject {info.node_id} != node {n1['identity']}")
    print("oracle: user identity active, contract issuer/subject correct")


asyncio.run(main())
