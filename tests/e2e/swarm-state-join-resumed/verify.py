#!/usr/bin/env python3
"""Oracle (criterion 2.1, step 3 of 3): a node that asked to join while
the User confirmed nothing is admitted when the User confirms after a
restart of either side, within the JoinRequest's hour.

- the request the User confirmed is the one node3 filed in step 1, before
  both restarts, and the confirmation came before its Timeout;
- user.join answered node3's membership: issued by the user key for node3
  at tier privileged_member, naming that request as its RequestID;
- node1 lists the request fulfilled; node3's active contract is that
  membership, and node3 sees the swarm's requests as a member
  (user.swarm_requests asks SeeSwarm): it is admitted.
"""
import asyncio
from datetime import datetime

import astral

from lib import swarmstate as ss
from lib.sessionio import load


def ts(s: str) -> float:
    # why: the node writes nanoseconds; datetime takes microseconds
    head, _, frac = s.rstrip("Z").partition(".")
    return datetime.fromisoformat(head + "+00:00").timestamp() + float("0." + (frac or "0"))


async def main():
    doc = load()
    n1, n3 = doc["nodes"]["node1"], doc["nodes"]["node3"]
    first = doc["facts"]["join_unanswered"]["request_id"]
    f = doc["facts"]["join_resumed"]
    user_id = doc["facts"]["user_id"]
    assert f["request_id"] == first, (
        f"the User confirmed {f['request_id']}, not the request filed before the restarts {first}")
    assert f["confirmed_at"] < ts(f["timeout"]), "the confirmation came after the request's Timeout"

    joined = f["joined"]
    assert joined and joined[0]["Type"] == "mod.auth.signed_contract", f"user.join answered {joined}"
    c = joined[0]["Object"]["Contract"]
    assert c["Issuer"] == user_id and c["Subject"] == n3["identity"], (
        f"membership {c['Issuer']} -> {c['Subject']}; want the User -> node3")
    assert [p["Action"] for p in c["Permits"]] == ["mod.user.privileged_member_action"], (
        f"membership permits {c['Permits']}; want the privileged_member tier")
    assert c.get("RequestID") == first, f"the membership names {c.get('RequestID')}, not {first}"

    async with await astral.connect(n1["endpoint"], token=doc["facts"]["user_token"]) as cu:
        status = await ss.status_of(cu, first)
    assert status == "fulfilled", f"node1 lists the request as {status}"
    async with await astral.connect(n3["endpoint"], token=n3["token"]) as c3:
        info = await c3.user.info()
        # why the contract's issuer: astral-py reads the user from the active
        # contract's issuer, which for a privileged_member is the user key.
        assert info.contract is not None and str(info.node_id) == n3["identity"] \
            and str(info.contract.contract.issuer) == user_id, (
            f"node3's active contract is not its membership from the User: {info.contract}")
        # why: user.swarm_requests asks SeeSwarm, which node3 allows itself
        # only when its own SwarmView holds its membership under the User.
        await ss.requests(c3)
    print("oracle: node3 admitted through the request it filed before both restarts")


asyncio.run(main())
