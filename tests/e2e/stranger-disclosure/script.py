#!/usr/bin/env python3
"""Driver: what a linked stranger reads of node1's User, by name and by contract.

The driver acts and judges nothing. The stranger, a node claimed by another
User and linked to node1, asks node1 the two questions that name
the User: `user.contract` and `dir.resolve localuser`. A third asker, the User
itself on node1, calls `user.contract` locally, where no link exists. The
driver records each answer.

why a stranger of its own, claimed here, and not swarm-authz-matrix's: a test
that continues another stops being an observer, and the planner then forks the
shared world (a golden plan and a selftest hold main.suite to one world and a
world per destroyer). This test is a sibling of the matrix under adopt-node
and builds the same fixture, a node claimed by a second User and linked to
node1 from its own side.
why the stranger's node token: a stranger's own User token is rejected by node1
before any op runs (swarm-authz-matrix, stranger_user), so only the node token
reaches the two ops.
"""
import asyncio

import astral
from astral.errors import AstralError
from astral.objectid import object_id_of

from lib.sessionio import load, write_facts

# why a deadline: a query that is never routed hangs rather than answering.
DEADLINE = 15


async def claim_and_link(n1: dict, s: dict) -> None:
    """A second User claims the stranger; the stranger links to node1 as itself.

    why a token-less bootstrap: it is how a second, unrelated User claims a
    node. why the stranger dials and node1 learns nothing: node1 should hold no
    record of the stranger it did not have before.
    """
    async with await astral.connect(s["endpoint"]) as anon:
        await anon.user.bootstrap(None)
    async with await astral.connect(s["endpoint"], token=s["token"]) as c:
        await c.nodes.add_endpoint(n1["identity"], n1["lan_endpoint"],
                                   experimental=True)
        await c.nodes.new_link(n1["identity"], endpoint=n1["lan_endpoint"],
                               strategies="basic", experimental=True,
                               timeout=60.0)


def describe(signed) -> dict:
    ct = signed.contract
    return {
        "id": str(object_id_of(signed)),
        "issuer": str(ct.issuer),
        "subject": str(ct.subject),
        "permits": sorted(p.action for p in ct.permits if p),
        "expires_at": int(ct.expires_at),
        "text": str(ct),
    }


async def attempt(thunk, shape=str) -> dict:
    """One query's outcome: what it answered, or how it was refused.

    why recorded and not raised: whether the stranger is answered is the
    oracle's finding, so a refusal must reach it as a fact.
    """
    try:
        return {"answered": True, "value": shape(await asyncio.wait_for(thunk(),
                                                                         DEADLINE))}
    except AstralError as e:
        return {"answered": False, "detail": f"{type(e).__name__}: {e}"[:200]}


async def main():
    doc = load()
    n1, s = doc["nodes"]["node1"], doc["nodes"]["sdstranger"]
    user_token = doc["facts"]["user_token"]

    await claim_and_link(n1, s)

    t = {"target": n1["identity"]}
    async with await astral.connect(s["endpoint"], token=s["token"]) as c:
        contract = await attempt(lambda: c.user.contract(**t), describe)
        localuser = await attempt(lambda: c.dir.resolve("localuser", **t))

    # the User's session calls as the User, which holds no link to node1
    async with await astral.connect(n1["endpoint"], token=user_token) as c:
        unlinked = await attempt(lambda: c.user.contract(), describe)

    write_facts({"disclosed": {"contract": contract, "localuser": localuser,
                               "unlinked": unlinked}})
    print(f"driver: stranger user.contract {contract}; "
          f"dir.resolve localuser {localuser}; "
          f"the User's own session: {unlinked}"[:600])


asyncio.run(main())
