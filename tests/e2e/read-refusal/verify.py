#!/usr/bin/env python3
"""Oracle: a stranger gets nothing, and the User still gets everything.

Four ops hand out or destroy object bytes — `objects.read`, `objects.load`,
`objects.contains`, `objects.delete` — and one authorizer is supposed to decide
all of them: the User and the local swarm are served, everyone else is refused.
The stranger is probed against each, and the User's own read is checked in the
same run, so a node that refuses everybody cannot pass either.

All four probes run before anything is asserted. A stranger that gets through
two ops and is stopped by a third is one finding, not two runs, and the failure
should name every op that leaked rather than the first one.

**A refusal here means a query rejection and nothing else.** `objects.read`
denies by `q.Reject()`, which arrives as `QueryRejected`, and that is the shape
the other three are expected to grow. Counting any `SessionError` would be a
hole big enough to walk an op through: `RouteNotFound` is a `SessionError`, so
an op that was renamed or dropped would answer "no route", read as a refusal,
and turn this test green having never been exercised at all.

Anything else the stranger gets back is a leak, including a polite one. An op
that accepts the query and answers `ack`, or answers `False`, has still decided
the stranger is someone worth serving; only the delete probe is additionally
judged by its effect, because there the bytes are the thing that survives.
"""
import asyncio

import astral
from astral.errors import AstralError, QueryRejected
from astral.objectid import object_id_of_bytes

from lib.sessionio import load

OPS = ("objects.read", "objects.load", "objects.contains", "objects.delete")


async def probe(coro):
    """Run one stranger call.

    Returns None when the node rejected the query, else a one-line account of
    what the stranger got — which is the leak, and is what the failure prints.

    why the wrapper: `objects.delete` answers `None` on success, so a bare
    return value cannot tell "deleted it" from "refused". The presence of the
    account is the signal; its contents are for the reader.
    """
    try:
        got = await coro
    except QueryRejected:
        return None
    except AstralError as e:
        # Not a rejection: a repository error, a transport failure, a wire
        # fault. None of those is the node deciding this identity may not
        # look, so none of them earns a pass.
        return f"{type(e).__name__}: {e}"
    return f"answered {got!r}" if got is not None else "accepted the query"


async def main():
    doc = load()
    n1 = doc["nodes"]["node1"]
    facts = doc["facts"]
    object_id = facts["object_id"]
    decoy_id = facts["decoy_id"]
    guest_token = facts["guest_token"]
    user_token = facts["user_token"]

    # The User's own read, first: ground truth that there is something to leak,
    # and a control that the op works at all under the same repo argument the
    # stranger uses. The id is a content hash and object-store writes an
    # untyped blob, so the bytes prove themselves against the id that asked
    # for them — this oracle needs no payload file of its own.
    async with await astral.connect(n1["endpoint"], token=user_token) as c:
        mine = await c.objects.read(object_id, repo="local")
    assert mine, f"the User read no bytes for {object_id}"
    assert str(object_id_of_bytes(mine)) == object_id, (
        f"the User's read of {object_id} hashes to "
        f"{object_id_of_bytes(mine)} — not the object it asked for")

    # The stranger, against all four ops. connect() authenticates eagerly, so a
    # token the node will not accept raises here rather than being miscounted
    # as four polite refusals.
    async with await astral.connect(n1["endpoint"], token=guest_token) as c:
        served = {
            "objects.read": await probe(c.objects.read(object_id, repo="local")),
            "objects.load": await probe(c.objects.load(object_id, repo="local")),
            "objects.contains": await probe(c.objects.contains("local", object_id)),
            "objects.delete": await probe(c.objects.delete("local", decoy_id)),
        }

    # Did the delete land? The decoy's survival is a second, independent
    # verdict on that probe — an op may refuse and delete anyway.
    async with await astral.connect(n1["endpoint"], token=user_token) as c:
        decoy_survived = await c.objects.contains("local", decoy_id)

    leaked = [f"{op} ({served[op]})" for op in OPS if served[op] is not None]
    if not decoy_survived:
        leaked.append("objects.delete destroyed the decoy")

    assert not leaked, (
        "a stranger — neither the User nor a swarm node — was not refused by: "
        + "; ".join(leaked))

    print(f"oracle: the stranger was rejected by all {len(OPS)} ops and the "
          f"decoy survived; the User still reads {object_id[:16]}… "
          f"({len(mine)} B) in full")


asyncio.run(main())
