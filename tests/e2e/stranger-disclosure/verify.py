#!/usr/bin/env python3
"""Oracle: what a linked stranger reads of node1's User is what the docs list,
and no more.

astral-docs topics/node-exposure.md ("Linked peers") and protocols/user/ops/
user.contract.md ("Disclosure") name the exposure: any directly linked peer
reads node1's active contract, which names the User, node1, the permits and the
expiry, and dir.resolve is open to it, so `localuser` names the User. The
swarm's roster is not in either answer. The roster ops (user.info,
user.swarm_status, user.list_siblings) refuse the stranger in
swarm-authz-matrix and are not asserted again here.

The oracle measures node1 itself, as the User, and compares: the contract the
stranger read is node1's active contract, byte for byte (same object id, same
fields), its issuer is the User the world was claimed with, `localuser`
resolves to that same identity, and neither answer carries node2 or any other
identity but those two. The User's own session, which holds no link to node1,
is refused `not permitted`, the one guard user.contract has.
"""
import asyncio
import time

import astral
from astral.objectid import object_id_of

from lib.sessionio import load

DEADLINE = 15
DAY = 86400


async def main():
    doc = load()
    nodes, facts = doc["nodes"], doc["facts"]
    n1, n2, s = nodes["node1"], nodes["node2"], nodes["sdstranger"]
    user_id = facts["user_id"]
    seen = facts["disclosed"]
    # why asserted before anything is compared: a refusal is the finding, and
    # it names which of the two disclosures the docs list is missing
    assert seen["contract"]["answered"], (
        f"node1 refused the linked stranger user.contract, which the docs list "
        f"as open to any linked peer: {seen['contract']['detail']}")
    assert seen["localuser"]["answered"], (
        f"node1 refused the linked stranger dir.resolve localuser, which the "
        f"docs list as open to any linked peer: {seen['localuser']['detail']}")
    contract, localuser = seen["contract"]["value"], seen["localuser"]["value"]
    unlinked = seen["unlinked"]

    # node1's own truth, read as the User
    async with await astral.connect(n1["endpoint"], token=facts["user_token"]) as u:
        info = await asyncio.wait_for(u.user.info(), DEADLINE)
    ac = info.contract
    assert ac is not None and ac.contract is not None, "node1 has no active contract"
    live = ac.contract
    assert str(live.issuer) == user_id and str(live.subject) == n1["identity"], (
        f"node1's active contract is {live.issuer} -> {live.subject}, not "
        f"{user_id} -> {n1['identity']}")

    # the exposure the docs list: the active contract, whole
    assert contract["id"] == str(object_id_of(ac)), (
        f"the stranger read contract {contract['id'][:20]}…, not node1's active "
        f"contract {str(object_id_of(ac))[:20]}…")
    assert contract["issuer"] == user_id, (
        f"the contract the stranger read names {contract['issuer']} as the "
        f"User, not {user_id}")
    assert contract["subject"] == n1["identity"], (
        f"the contract the stranger read is for {contract['subject']}, "
        f"not node1")
    assert contract["permits"] == sorted(p.action for p in live.permits if p), (
        f"permits the stranger read: {contract['permits']}")
    assert contract["permits"], "node1's contract carries no permit to disclose"
    assert contract["expires_at"] == int(live.expires_at)
    # why: user.contract.md "Disclosure" — the expiry, 365 days by default,
    # shows when the User claimed or adopted the node
    # expires_at is nanoseconds since the epoch
    ttl = contract["expires_at"] / 1e9 - time.time()
    assert 300 * DAY < ttl < 400 * DAY, (
        f"the contract expires in {ttl / DAY:.0f} days, not about 365")

    # the second name for the User
    assert localuser == user_id, (
        f"dir.resolve localuser answered {localuser} to the stranger, not the "
        f"User {user_id}")

    # nothing of the roster rides along: the swarm's other node, and the
    # stranger's own User, appear in neither answer
    for name, ident in (("node2", n2["identity"]), ("stranger", s["identity"])):
        assert ident not in contract["text"] and ident != localuser, (
            f"{name} {ident[:20]}… appears in what the stranger read")

    # the one guard: a session with no link to node1 is not answered
    assert not unlinked["answered"], (
        f"the User's own session, which holds no link, read user.contract")
    assert "not permitted" in unlinked["detail"], (
        f"the User's own session was refused for another reason: "
        f"{unlinked['detail']}")

    # the precondition: the stranger is linked to node1 and is another User's
    async with await astral.connect(s["endpoint"], token=s["token"]) as c:
        links = await asyncio.wait_for(c.nodes.links(experimental=True), DEADLINE)
        assert any(str(l.remote_identity) == n1["identity"] for l in links), (
            "the stranger holds no link to node1")
        sinfo = await asyncio.wait_for(c.user.info(), DEADLINE)
    owner = str(sinfo.contract.contract.issuer) if sinfo.contract else None
    assert sinfo.state == "claimed" and owner and owner != user_id, (
        f"the stranger is {sinfo.state}, owned by {owner}; it must belong to "
        f"another User than {user_id}")

    print(f"oracle: the linked stranger read node1's active contract "
          f"({len(contract['permits'])} permit(s), expiring in "
          f"{ttl / DAY:.0f} days) naming User {user_id[:16]}…, and "
          "dir.resolve localuser named the same User; neither carried another "
          "identity; the User's own unlinked session was refused")


asyncio.run(main())
