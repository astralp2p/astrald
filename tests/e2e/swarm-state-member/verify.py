#!/usr/bin/env python3
"""Oracle: node3 is a Member of the User's swarm, admitted in the carrier
session.

- node3's membership names node3, is issued by node1 (its carrier) and
  names node3's JoinRequest as its RequestID (astral-docs
  protocols/user/ops/user.join.md);
- node3's active contract is that membership (user.info), and node3 sees
  the swarm's requests as a member (user.swarm_requests asks SeeSwarm);
- node1 lists node3's JoinRequest as fulfilled (user.swarm_requests).
"""
import asyncio

import astral

from lib import swarmstate as ss
from lib.sessionio import load


async def main():
    doc = load()
    nodes, f = doc["nodes"], doc["facts"]["member_join"]
    n1, n3 = nodes["node1"], nodes["node3"]
    m = f["membership"]
    assert m.get("Subject") == n3["identity"], f"membership names {m.get('Subject')}, not node3"
    assert m.get("Issuer") == n1["identity"], f"membership issued by {m.get('Issuer')}, not node1"
    assert f.get("membership_request") == f["request_id"], (
        f"node3's membership names {f.get('membership_request')}, not its JoinRequest {f['request_id']}")

    async with await astral.connect(n3["endpoint"], token=n3["token"]) as c3:
        info = await c3.user.info()
        # why not info.user_id: user.info names no user, and astral-py reads
        # the active contract's issuer, which for a Member is its carrier.
        assert info.contract is not None and str(info.node_id) == n3["identity"] \
            and str(info.contract.contract.issuer) == n1["identity"], (
            f"node3's active contract is not its membership from node1: {info.contract}")
        # why: user.swarm_requests asks SeeSwarm, which node3 allows itself
        # only when its own SwarmView holds its membership under the User.
        await ss.requests(c3)
    async with await astral.connect(n1["endpoint"], token=doc["facts"]["user_token"]) as c:
        status = await ss.status_of(c, f["request_id"])
    assert status == "fulfilled", f"node1 lists node3's JoinRequest as {status}"
    print("oracle: node3 is a Member, admitted by node1's Approval")


asyncio.run(main())
