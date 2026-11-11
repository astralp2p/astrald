#!/usr/bin/env python3
"""Oracle: User B owns a two-node swarm, acn-b1 and acn-b2, apart from the first User's.

- acn-b1 and acn-b2 are `claimed`, both under User B, and not under the first
  User; acn-b2's contract is the one the driver recorded;
- each lists the other as a linked sibling (the adoption's sync and link are
  asynchronous, so within a window);
- node1, the first User's node, lists neither.
"""
import asyncio
import time

import astral

from lib.sessionio import load

CONVERGE = 30.0
POLL = 1.0


def linked(members) -> set:
    return {str(m.identity) for m in members
            if m.linked and m.identity is not None}


def roster(members) -> set:
    return {str(m.identity) for m in members if m.identity is not None}


async def until(probe, want):
    deadline = time.monotonic() + CONVERGE
    got = await probe()
    while not want(got) and time.monotonic() < deadline:
        await asyncio.sleep(POLL)
        got = await probe()
    return got


async def main():
    doc = load()
    n1, b1, b2 = (doc["nodes"][n] for n in ("node1", "acn-b1", "acn-b2"))
    facts = doc["facts"]
    a_id, b_id = facts["user_id"], facts["acn_user_id"]
    assert b_id != a_id, "User B is the first User"

    for name, n, other in (("acn-b1", b1, b2), ("acn-b2", b2, b1)):
        async with await astral.connect(n["endpoint"], token=n["token"]) as c:
            i = await c.user.info()
            assert i.state == "claimed" and str(i.user_id) == b_id, (
                f"{name} is {i.state!r} under {i.user_id!s}, not claimed by "
                f"User B {b_id}")
            if name == "acn-b2":
                assert str(i.contract_id) == facts["acn_contract_id"], (
                    f"acn-b2 holds contract {i.contract_id}, the driver "
                    f"recorded {facts['acn_contract_id']}")

            async def view():
                return linked(await c.user.swarm_status())
            sibs = await until(view, lambda s: other["identity"] in s)
            assert other["identity"] in sibs, (
                f"{name} has no linked sibling {other['identity']} within "
                f"{CONVERGE:.0f} s; linked: {sorted(sibs) or 'none'}")
            members = roster(await c.user.swarm_status())
            assert n1["identity"] not in members, (
                f"{name}'s roster {sorted(members)} holds the first User's node1")

    async with await astral.connect(n1["endpoint"], token=facts["user_token"]) as c:
        members = roster(await c.user.swarm_status())
    assert not ({b1["identity"], b2["identity"]} & members), (
        f"node1's roster {sorted(members)} holds a node of User B's swarm")

    print(f"oracle: User B {b_id[:8]}… owns acn-b1 and acn-b2, linked; "
          "node1's swarm holds neither")


asyncio.run(main())
