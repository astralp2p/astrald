#!/usr/bin/env python3
"""Oracle: refused adoptions answer cleanly and leave the swarm untouched.

From the driver's recorded answers (facts) and the oracle's own live reads:

- `user.adopt` of an unknown name answers `unknown identity`; of the zero
  identity, by name and by its 66 zeros, `zero identity` (op_adopt.go);
- adopt of node2, already a member, is refused, never answered with a second
  contract: node1 signs, node2's accept_membership refuses `node already
  claimed` (op_accept_membership.go:37), and node1 indexes nothing. The
  refusal reaches the User as `node already claimed` or, when node2 closes
  the session before node1 has written the contract to it, as
  `session closed` (mod/nodes/src/session.go:93): either is the refusal,
  and the race between them is astrald's, seen in about half the runs;
- the roster node1 reads (as the User) and the roster node2 reads (with its
  token) hold exactly the identities they held before, and still list both
  nodes;
- each node's active contract is the same object it was: the contract node2
  holds as node1 reads it, and the one node1 holds as node2 reads it.
"""
import asyncio

import astral
from astral.objectid import object_id

from lib.sessionio import load

EXPECT = {
    "unknown": "unknown identity",
    "zero_name": "zero identity",
    "zero_hex": "zero identity",
    "readopt": ("node already claimed", "session closed"),
}


async def main():
    doc = load()
    n1, n2 = doc["nodes"]["node1"], doc["nodes"]["node2"]
    f = doc["facts"]
    ids = {n1["identity"], n2["identity"]}

    for name, want in EXPECT.items():
        got = f["answers"][name]
        want = (want,) if isinstance(want, str) else want
        assert got["outcome"] == "refused" and got["message"] in want, (
            f"adopt {name}: want refusal {' or '.join(want)!r}, got {got['outcome']} "
            f"{got['message']!r}")

    before = f["roster_before"]
    assert ids <= set(before["node1"]) and ids <= set(before["node2"]), (
        f"before the attempts the swarm did not hold both nodes: {before}")

    async with await astral.connect(n1["endpoint"], token=f["user_token"]) as c:
        r1 = sorted(str(m.identity) for m in await c.user.swarm_status())
    async with await astral.connect(n2["endpoint"], token=n2["token"]) as c:
        r2 = sorted(str(m.identity) for m in await c.user.swarm_status())
        k1 = str(object_id(await c.user.contract(target=n1["identity"])))
    async with await astral.connect(n1["endpoint"], token=n1["token"]) as c:
        k2 = str(object_id(await c.user.contract(target=n2["identity"])))

    assert r1 == before["node1"], f"node1 roster changed: {before['node1']} -> {r1}"
    assert r2 == before["node2"], f"node2 roster changed: {before['node2']} -> {r2}"
    kb = f["contracts_before"]
    assert k1 == kb["node1"], f"node1's contract changed: {kb['node1']} -> {k1}"
    assert k2 == kb["node2"], f"node2's contract changed: {kb['node2']} -> {k2}"

    print("oracle: unknown, zero and re-adopt refused; rosters and contracts unchanged")


asyncio.run(main())
