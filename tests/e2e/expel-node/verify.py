#!/usr/bin/env python3
"""Oracle: the ban is recorded and the roster shrank.

Ported from the netsim expel-node verify.py: node2 must appear in the User's
user.list_expelled and must be gone from user.swarm_status, whose ActiveNodes
filters the expelled set. Link state is not asserted.

why: node2's identity comes from session.json, not from node2 — an expelled
node refuses user.info, so it is not a usable identity source afterwards.
"""
import asyncio

import astral

from lib.sessionio import load


async def main():
    doc = load()
    n1, n2 = doc["nodes"]["node1"], doc["nodes"]["node2"]
    user_id = doc["facts"]["user_id"]

    # why: list_expelled and swarm_status require the contract issuer, so both
    # run under the User token rather than node1's own.
    async with await astral.connect(n1["endpoint"],
                                    token=doc["facts"]["user_token"]) as c:
        expelled = await c.user.list_expelled()
        members = await c.user.swarm_status()

    banned = {str(e.expulsion.subject) for e in expelled
              if e.expulsion is not None and e.expulsion.subject is not None}
    issuers = {str(e.expulsion.issuer) for e in expelled
               if e.expulsion is not None and e.expulsion.issuer is not None}
    roster = {str(m.identity) for m in members if m.identity is not None}

    assert n2["identity"] in banned, (
        f"node2 {n2['identity']} is not in user.list_expelled {banned} — "
        "the expulsion was never issued")
    assert user_id in issuers, (
        f"the ban was not issued by the User {user_id}: issuers {issuers}")
    assert n2["identity"] not in roster, (
        f"node2 {n2['identity']} still appears in user.swarm_status {roster} — "
        "the roster did not shrink")

    print(f"oracle: User {user_id[:8]}… banned node2 {n2['identity'][:8]}…; "
          f"listed as expelled and dropped from the roster "
          f"({len(roster)} member(s) remain)")


asyncio.run(main())
