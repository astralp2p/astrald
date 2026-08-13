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

The delete probe is judged by its effect, never by its answer: the decoy is
still there afterwards, or the stranger deleted it. An op that answers `ack`
and does nothing and an op that refuses are the same verdict here — the bytes
survived — and that is the property worth holding.
"""
import asyncio

import astral
from astral.errors import AstralError
from astral.objectid import object_id_of_bytes

from lib.sessionio import load

OPS = ("objects.read", "objects.load", "objects.contains", "objects.delete")


async def probe(coro):
    """Run one stranger call. Returns None when refused, else what came back."""
    try:
        return {"served": await coro}
    except AstralError as e:
        return None if _is_refusal(e) else {"served": f"unexpected {e!r}"}


def _is_refusal(e: BaseException) -> bool:
    """A refusal is the node saying no, not the harness failing to ask.

    why: a transport or wire failure would otherwise read as a green refusal —
    the loudest possible false pass for a test whose whole subject is denial.
    """
    from astral.errors import SessionError
    return isinstance(e, SessionError)


async def main():
    doc = load()
    n1 = doc["nodes"]["node1"]
    facts = doc["facts"]
    object_id = facts["object_id"]
    decoy_id = facts["decoy_id"]
    guest_token = facts["guest_token"]
    user_token = facts["user_token"]

    # The User's own read, first: ground truth that there is something to leak.
    # The id is a content hash, so the bytes prove themselves — this oracle
    # needs no payload file and never asks the driver what was stored.
    async with await astral.connect(n1["endpoint"], token=user_token) as c:
        mine = await c.objects.read(object_id, repo="local")
    assert mine, f"the User read no bytes for {object_id}"
    assert str(object_id_of_bytes(mine)) == object_id, (
        f"the User's read of {object_id} hashes to "
        f"{object_id_of_bytes(mine)} — not the object it asked for")

    # The stranger, against all four ops.
    async with await astral.connect(n1["endpoint"], token=guest_token) as c:
        served = {
            "objects.read": await probe(c.objects.read(object_id, repo="local")),
            "objects.load": await probe(c.objects.load(object_id, repo="local")),
            "objects.contains": await probe(c.objects.contains("local", object_id)),
            "objects.delete": await probe(c.objects.delete("local", decoy_id)),
        }

    # Did the delete land? The decoy's survival is the verdict, not the answer.
    async with await astral.connect(n1["endpoint"], token=user_token) as c:
        decoy_survived = await c.objects.contains("local", decoy_id)

    leaked = [op for op in OPS if served[op] is not None]
    if not decoy_survived:
        leaked = [op for op in leaked if op != "objects.delete"]
        leaked.append("objects.delete (the decoy is gone)")

    assert not leaked, (
        "a stranger — neither the User nor a swarm node — was served by "
        + ", ".join(leaked)
        + f"; only {len(OPS) - len(leaked)} of {len(OPS)} refused it")

    print(f"oracle: the stranger was refused by all {len(OPS)} ops and the "
          f"decoy survived; the User still reads {object_id[:16]}… "
          f"({len(mine)} B) in full")


asyncio.run(main())
