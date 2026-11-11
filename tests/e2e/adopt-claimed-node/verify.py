#!/usr/bin/env python3
"""Oracle: a node User B owns refuses the first User's adoption, and stays B's.

Judged from the driver's recorded outcome and the nodes' own state:

- the first User's `user.adopt` of acn-b2 was refused, as "node already
  claimed" (user.ErrAlreadyClaimed, answered by OpAcceptMembership while the
  node holds an active contract) or as "session closed" (see REFUSALS);
- acn-b2 is still `claimed` under User B, holds the contract it held before
  (same contract id, issued by B) and lists acn-b1, never node1;
- acn-b1 still lists acn-b2 in B's roster;
- node1, as the first User, does not list acn-b2.
"""
import asyncio

import astral

from lib.sessionio import load

# why two messages: OpAcceptMembership answers ErrAlreadyClaimed before it
# reads the contract, and the inviter's client sends the contract first
# (astral-go api/user/client/accept_membership.go). When the node has
# already closed the channel, that send fails and adopt answers "session
# closed" instead of the node's reason. Measured on 20 attempts against
# one claimed node: 3 "node already claimed", 17 "session closed", the
# same through astral-py and the JSON op. Either way the node refused;
# an adoption that succeeds, or fails for another reason (unreachable,
# not permitted), is neither.
REFUSALS = ("node already claimed", "session closed")


def roster(members) -> set:
    return {str(m.identity) for m in members if m.identity is not None}


async def main():
    doc = load()
    n1, b1, b2 = (doc["nodes"][n] for n in ("node1", "acn-b1", "acn-b2"))
    facts = doc["facts"]
    a_id, b_id = facts["user_id"], facts["acn_user_id"]
    out = facts["acn_adopt_outcome"]
    print(f"oracle: driver recorded {out}")

    # why text: the fact is what an operator can write too, the answer astral
    # gave it, so the driver's "refused: <error>" and an operator's own
    # words are judged the same way
    assert not out.startswith("answered"), (
        f"the first User's user.adopt of acn-b2 was answered ({out}); a "
        "claimed node must refuse")
    assert any(r in out for r in REFUSALS), (
        f"adopt of a claimed node was refused, but not as one of {REFUSALS}: "
        f"{out!r}")

    # acn-b2, with its own token: the question is what the node holds
    async with await astral.connect(b2["endpoint"], token=b2["token"]) as c:
        i = await c.user.info()
        members2 = roster(await c.user.swarm_status())
    assert i.state == "claimed", f"acn-b2 is {i.state!r}, not claimed"
    assert str(i.user_id) == b_id, (
        f"acn-b2 belongs to {i.user_id!s}, not User B {b_id} "
        f"(the first User is {a_id})")
    assert str(i.contract_id) == facts["acn_contract_id"], (
        f"acn-b2's contract is {i.contract_id}, no longer the recorded "
        f"{facts['acn_contract_id']}")
    assert b1["identity"] in members2 and n1["identity"] not in members2, (
        f"acn-b2's roster {sorted(members2)} is not B's (acn-b1 in, node1 out)")

    async with await astral.connect(b1["endpoint"], token=facts["acn_user_token"]) as c:
        members_b = roster(await c.user.swarm_status())
    assert {b1["identity"], b2["identity"]} <= members_b, (
        f"User B's roster {sorted(members_b)} lost acn-b2")

    async with await astral.connect(n1["endpoint"], token=facts["user_token"]) as c:
        members_a = roster(await c.user.swarm_status())
    assert b2["identity"] not in members_a, (
        f"the first User's roster {sorted(members_a)} lists acn-b2")

    print(f"oracle: adopt of acn-b2 refused ({out}); acn-b2 "
          f"still User B's ({b_id[:8]}…) under the same contract, in B's "
          "roster and not in the first User's")


asyncio.run(main())
