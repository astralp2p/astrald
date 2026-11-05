#!/usr/bin/env python3
"""Driver: the User bans node3 from a three-node swarm, and node3 tries back.

1. The User stores a bait object on node1, readable by any member.
2. The User expels node3 on node1.
3. node3, with its own token, waits until it no longer reads itself as
   `claimed`, then probes: read the bait from node1, join node1's swarm,
   and list its own repositories.
4. The User tries to adopt node3 again.

The outcomes are recorded as the `probes` fact, for the report. The oracle
judges by its own reads, never by these.
"""
import asyncio
import time

import astral
from astral.object import Query

from lib.sessionio import load, write_facts

LEAVE = 30.0       # how long node3 may take to read the ban that reaches it


async def outcome(coro, timeout=20.0) -> str:
    try:
        r = await asyncio.wait_for(coro, timeout)
    except Exception as e:  # noqa: BLE001 — every refusal is an outcome here
        return f"refused: {type(e).__name__}: {e}"
    return f"answered: {str(r)[:120]}"


async def node_state(c) -> str:
    """node3's state as node3 reads it, `leaving` while it cannot say.

    why: between storing the ban on itself and clearing its contract
    (expel.go receiveExpulsion -> leaveSwarm), node3 is still claimed but no
    longer a member, so SeeSwarm refuses its own token and user.info answers
    "not permitted" (op_info.go). Seen once in five runs; it is the leave in
    progress, not an outcome.
    """
    try:
        return (await c.user.info()).state
    except astral.errors.RemoteError as e:
        if e.message != "not permitted":
            raise
        return "leaving"


async def main():
    doc = load()
    n1, n3 = doc["nodes"]["node1"], doc["nodes"]["node3"]
    user_token = doc["facts"]["user_token"]
    id1, id3 = n1["identity"], n3["identity"]

    async with await astral.connect(n1["endpoint"], token=user_token) as c:
        (bait,) = await c.objects.store(
            Query(query_string="expel-propagation bait"), repo="local")
        bait = str(bait)
        signed = await c.user.expel(id3)
    expelled = str(signed.expulsion.subject)

    probes = {}
    async with await astral.connect(n3["endpoint"], token=n3["token"]) as c:
        deadline = time.monotonic() + LEAVE
        state = await node_state(c)
        while state in ("claimed", "leaving") and time.monotonic() < deadline:
            await asyncio.sleep(0.5)
            state = await node_state(c)
        probes["node3_state"] = state
        probes["read_bait_on_node1"] = await outcome(
            c.objects.read(bait, target=id1))
        probes["join_node1"] = await outcome(c.user.join(id1))
        probes["own_repositories"] = await outcome(c.objects.repositories())

    async with await astral.connect(n1["endpoint"], token=user_token) as c:
        probes["readopt_node3"] = await outcome(c.user.adopt(id3))

    write_facts({"expelled_id": expelled, "bait_id": bait, "probes": probes})
    for k, v in probes.items():
        print(f"driver: {k}: {v}")
    print(f"driver: node3 {expelled[:16]}… expelled; bait {bait[:16]}…")


asyncio.run(main())
