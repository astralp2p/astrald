#!/usr/bin/env python3
"""Driver: the User asks node1 to adopt three identities it must not adopt.

In the adopt-node swarm (node1 and node2, one User), as the User on node1:

1. snapshot both nodes' rosters and the id of each node's active contract;
2. `user.adopt` an alias nothing carries, the zero identity (as the name
   `anyone` and as 66 zeros), and node2 again, which is already a member;
3. record each answer as the `answers` fact.

The oracle judges the answers and reads the swarm itself; this driver never
decides whether the refusals are right or the roster held.
"""
import asyncio

import astral
from astral.objectid import object_id

from lib.sessionio import load, write_facts

# why: adopt on an unknown name is answered by the directory lookup, on the
# zero identity by the op, so each is its own case; the zero identity is
# reached by name (`anyone`, resolved by the node) and by its 66 hex zeros.
UNKNOWN = "adopt-refusals-nobody"
ZERO_HEX = "0" * 66
# why: the swarm's contract and sync tasks run asynchronously after an adopt
# that went through; a refused adopt issues none, but a regression that
# lets one through would only show once its push has landed.
SETTLE = 3.0


async def adopt_outcome(c, node) -> dict:
    try:
        signed = await c.user.adopt(node, timeout=30.0)
    except astral.errors.RemoteError as e:
        return {"outcome": "refused", "message": e.message}
    except Exception as e:  # noqa: BLE001 — recorded, the oracle rejects it
        return {"outcome": "failed", "message": f"{type(e).__name__}: {e}"}
    return {"outcome": "answered", "message": str(object_id(signed))}


async def main():
    doc = load()
    n1, n2 = doc["nodes"]["node1"], doc["nodes"]["node2"]
    user_token = doc["facts"]["user_token"]

    # what the swarm looks like to each of its two nodes, and the contract each
    # holds (read by its peer: user.contract answers a directly linked node)
    async with await astral.connect(n1["endpoint"], token=user_token) as c:
        r1 = sorted(str(m.identity) for m in await c.user.swarm_status())
    async with await astral.connect(n2["endpoint"], token=n2["token"]) as c:
        r2 = sorted(str(m.identity) for m in await c.user.swarm_status())
        k1 = str(object_id(await c.user.contract(target=n1["identity"])))
    async with await astral.connect(n1["endpoint"], token=n1["token"]) as c:
        k2 = str(object_id(await c.user.contract(target=n2["identity"])))

    answers = {}
    async with await astral.connect(n1["endpoint"], token=user_token) as c:
        answers["unknown"] = await adopt_outcome(c, UNKNOWN)
        answers["zero_name"] = await adopt_outcome(c, "anyone")
        answers["zero_hex"] = await adopt_outcome(c, ZERO_HEX)
        answers["readopt"] = await adopt_outcome(c, n2["identity"])
    await asyncio.sleep(SETTLE)

    write_facts({"roster_before": {"node1": r1, "node2": r2},
                 "contracts_before": {"node1": k1, "node2": k2},
                 "answers": answers})
    for k, v in answers.items():
        print(f"driver: adopt {k}: {v['outcome']}: {v['message']}")


asyncio.run(main())
