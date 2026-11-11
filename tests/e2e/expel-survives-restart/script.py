#!/usr/bin/env python3
"""Driver: record the expelled swarm before all three nodes restart.

The harness restarts node1, node2 and node3 after this driver (test.toml
`restart`). Nothing is changed here: the driver waits until the ban that
`expel-propagation` issued has settled everywhere, so the oracle's "still
banned" compares against a ban that had landed:

- node1 and node2 each hold the User's ban on node3 and list only node1 and
  node2 in their rosters;
- node3 reads itself `unclaimed` (it left the swarm when the ban reached it).

It records `members_before`, the sorted identities of the two remaining
members, and `restarts_before`, each node's restart count so far.
"""
import asyncio
import time

import astral

from lib.sessionio import load, write_facts

CONVERGE = 30.0
POLL = 1.0


async def settled(n1, n2, n3, user_token, user_id, id3, remaining) -> dict:
    """What each node holds now, as the node's own token reads it."""
    async with await astral.connect(n1["endpoint"], token=user_token) as c:
        bans1 = {str(e.expulsion.subject): str(e.expulsion.issuer)
                 for e in await c.user.list_expelled()}
        roster1 = {str(m.identity) for m in await c.user.swarm_status()}
    async with await astral.connect(n2["endpoint"], token=n2["token"]) as c:
        bans2 = {str(e.expulsion.subject): str(e.expulsion.issuer)
                 for e in await c.user.list_expelled()}
        roster2 = {str(m.identity) for m in await c.user.swarm_status()}
    async with await astral.connect(n3["endpoint"], token=n3["token"]) as c:
        try:
            state3 = (await c.user.info()).state
        except astral.errors.RemoteError as e:
            if e.message != "not permitted":
                raise
            state3 = "leaving"
    return {"bans1": bans1, "bans2": bans2, "roster1": roster1,
            "roster2": roster2, "state3": state3, "user_id": user_id,
            "id3": id3, "remaining": remaining}


def whole(v) -> bool:
    return (v["bans1"].get(v["id3"]) == v["user_id"]
            and v["bans2"].get(v["id3"]) == v["user_id"]
            and v["roster1"] == v["remaining"]
            and v["roster2"] == v["remaining"]
            and v["state3"] == "unclaimed")


async def main():
    doc = load()
    n1, n2, n3 = (doc["nodes"][n] for n in ("node1", "node2", "node3"))
    facts = doc["facts"]
    id1, id2, id3 = n1["identity"], n2["identity"], n3["identity"]
    assert facts["expelled_id"] == id3, (
        f"expel-propagation expelled {facts['expelled_id']}, not node3 {id3}")
    remaining = {id1, id2}

    deadline = time.monotonic() + CONVERGE
    args = (n1, n2, n3, facts["user_token"], facts["user_id"], id3, remaining)
    v = await settled(*args)
    while not whole(v) and time.monotonic() < deadline:
        await asyncio.sleep(POLL)
        v = await settled(*args)
    assert whole(v), (
        f"the ban from expel-propagation has not settled before the restart: "
        f"node1 bans {v['bans1']}, node2 bans {v['bans2']}, rosters "
        f"{sorted(v['roster1'])} / {sorted(v['roster2'])}, node3 {v['state3']!r}")

    write_facts({
        "restarts_before": {n: doc["nodes"][n].get("restarts") or 0
                            for n in ("node1", "node2", "node3")},
        "members_before": sorted(remaining),
    })
    print("driver: node3 banned by the User on node1 and node2, out of both "
          "rosters and unclaimed; restarting all three")


asyncio.run(main())
