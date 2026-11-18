#!/usr/bin/env python3
"""Oracle: the re-sync traffic of a 10-node swarm with 5 bans, measured.

Judged by the oracle's own reads:
- every node holds the five bans issued by the User.

Judged on the driver's log counts (the measurement is the fact, and the PR
records it):
- each link event re-linked node2 and node3, and ran at least one
  SyncNodesTask (the sync runs on every link, not only the first);
- the quiet window pushed at least BANS bans to each of nine siblings: one
  sweep period of one node, the floor of a running sweep.
The rest of the counts are reported, not judged.
"""
import asyncio

import astral

from lib.sessionio import load

NAMES = [f"node{i}" for i in range(1, 11)]
BANS = 5


async def main():
    doc = load()
    facts = doc["facts"]
    subjects, user_id = set(facts["bans"]), facts["user_id"]
    print(f"oracle: link events {facts['link_events']}")
    print(f"oracle: sweep window {facts['sweep']}")

    assert len(subjects) == BANS, f"the driver issued {len(subjects)} bans; want {BANS}"
    for name in NAMES:
        n = doc["nodes"][name]
        async with await astral.connect(n["endpoint"], token=n["token"]) as c:
            held = {str(e.expulsion.subject): str(e.expulsion.issuer)
                    for e in await c.user.list_expelled()}
        missing = [s for s in subjects if held.get(s) != user_id]
        assert not missing, f"{name} lacks the User's bans on {missing}"

    for i, event in enumerate(facts["link_events"], 1):
        assert event["relinked"], f"link event {i}: node2 never re-linked to node3"
        assert event["sync_runs"] >= 1, (
            f"link event {i} ran no SyncNodesTask: {event}")

    sweep = facts["sweep"]
    assert sweep["ban_pushes"] >= BANS * (len(NAMES) - 1), (
        f"the quiet window pushed {sweep['ban_pushes']} bans; a running sweep "
        f"pushes at least {BANS * (len(NAMES) - 1)} per period")

    print("oracle: every node holds the five bans; the counts above are the measurement")


asyncio.run(main())
