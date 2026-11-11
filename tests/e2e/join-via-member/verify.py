#!/usr/bin/env python3
"""Oracle: a member without the User key cannot bring a node in, and leaves
nothing behind; the member that holds the key can, and the swarm agrees.

Two kinds of judgement. The driver's record first:

- the join through node2 was refused with the `sign as issuer` prefix, the
  text node2 gives when the issuer's key is not among its own (the contract
  is the User's, and only a key holder can sign it);
- once the refusal had had time to leave debris, the latecomer was still
  `fresh`, and neither node1's nor node2's roster held the latecomer, only the
  two nodes there before;
- the join through node1 was admitted, and the contract it returned names the
  User as issuer and the latecomer as subject.

Then what the nodes hold now, each asked on its own token: the latecomer is
claimed by the same User, and within a window `user.swarm_status` lists
exactly node1, node2 and the latecomer on every node, with `user.list_siblings`
and the linked members exactly the other two. Exact sets, so a roster that
omits the latecomer on a sibling, or lists a stranger, fails.
"""
import asyncio
import time

import astral

from lib.sessionio import load

# The prefix every setup client matches for a missing issuer key
# (mod/auth/src/signing.go SignIssuer); the rest names the key.
ISSUER = "sign as issuer"
# why a window: node1's push to node2 and the roster sync that follow an
# admission are asynchronous; node2 was never told where the latecomer is.
CONVERGE = 30.0
POLL = 1.0


def names(ids, by_id) -> str:
    return "[" + ", ".join(by_id.get(i, i[:12] + "…") for i in ids) + "]"


async def observe(c) -> dict:
    members = await c.user.swarm_status()
    sibs = await c.user.list_siblings()
    return {
        "members": sorted(str(m.identity) for m in members
                          if m.identity is not None),
        "linked": sorted(str(m.identity) for m in members
                         if m.linked and m.identity is not None),
        "siblings": sorted(str(s) for s in sibs),
    }


def check_record(f: dict, ids: dict, by_id: dict, user_id: str) -> None:
    r2 = f["join_via_node2"]
    assert r2["refused"], (
        "node2 admitted the latecomer: a member without the User key signed "
        f"as the issuer ({r2['text']!r})")
    assert r2["text"].startswith(ISSUER), (
        f"node2 refused the join with {r2['error']} {r2['text']!r}, not with "
        f"the {ISSUER!r} prefix: the refusal is not the missing issuer key's")

    after = f["after_refusal"]
    assert after["latecomer_state"] == "fresh", (
        f"the latecomer is {after['latecomer_state']!r} after the refused "
        "join, not fresh (a partial membership)")
    before = sorted([ids["node1"], ids["node2"]])
    for name, roster in after["rosters"].items():
        assert roster == before, (
            f"{name} lists {names(roster, by_id)} after the refused join, "
            f"not exactly {names(before, by_id)} (debris of the refusal)")

    r1 = f["join_via_node1"]
    assert not r1["refused"], (
        f"node1, which holds the User key, refused the latecomer: "
        f"{r1['error']} {r1['text']!r}")
    assert r1["issuer"] == user_id and r1["subject"] == ids["jvm-latecomer"], (
        f"the contract node1 issued names issuer {r1['issuer']} and subject "
        f"{r1['subject']}, not the User {user_id} and the latecomer")


async def main():
    doc = load()
    nodes = {n: doc["nodes"][n] for n in ("node1", "node2", "jvm-latecomer")}
    ids = {n: nodes[n]["identity"] for n in nodes}
    by_id = {v: k for k, v in ids.items()}
    roster = sorted(ids.values())
    user_id = doc["facts"]["user_id"]

    check_record(doc["facts"], ids, by_id, user_id)

    for name, node in nodes.items():
        others = sorted(set(roster) - {ids[name]})
        async with await astral.connect(node["endpoint"],
                                        token=node["token"]) as c:
            info = await c.user.info()
            assert info.state == "claimed" and str(info.user_id) == user_id, (
                f"{name} is {info.state!r} under {info.user_id!s}, not "
                f"claimed by the User {user_id}")

            deadline = time.monotonic() + CONVERGE
            got = await observe(c)
            while ((got["members"] != roster or got["linked"] != others
                    or got["siblings"] != others)
                   and time.monotonic() < deadline):
                await asyncio.sleep(POLL)
                got = await observe(c)
        assert got["members"] == roster, (
            f"{name}'s swarm_status lists {names(got['members'], by_id)}, not "
            f"exactly {names(roster, by_id)}, {CONVERGE:.0f} s after the "
            "latecomer joined via node1 (roster sync regression)")
        assert got["siblings"] == others, (
            f"{name}'s list_siblings is {names(got['siblings'], by_id)}, not "
            f"exactly {names(others, by_id)}")
        assert got["linked"] == others, (
            f"{name}'s swarm_status marks {names(got['linked'], by_id)} "
            f"linked, not exactly {names(others, by_id)}")

    print("oracle: join via node2 refused with 'sign as issuer', no debris; "
          "join via node1 admitted; all three nodes list exactly node1, "
          "node2 and the latecomer")


asyncio.run(main())
