#!/usr/bin/env python3
"""Driver: the F3 chain's join step; node5 joins the swarm with its own app.

node1 and node5 learn each other's endpoint (no other member learns node5's),
node5 joins through node1 as a Member, and the `f3` mesh app is spawned on
node5. The driver waits up to 60 s from the join until all 20 pairs of the
five apps answer since the join.

It judges nothing and records the `churn_join` fact: {joined_at, ready_at
(null when the 20 pairs did not answer within 60 s), missing, app (node5's
app id)}.
"""
import asyncio
import time

import astral

from lib import meshapps
from lib.sessionio import load, write_facts
from lib import swarmstate as ss

NAMES = ["node1", "node2", "node3", "node4", "node5"]
PREFIX = "f3"
RECOVER = 60.0


async def join(doc: dict) -> float:
    """node1 and node5 meet, and node5 joins through node1; the time of the
    join's answer."""
    n1, n5 = doc["nodes"]["node1"], doc["nodes"]["node5"]
    # note: node5 joins through node1 as a Member
    await ss.admit(n1, doc["facts"]["user_token"], n5)
    return time.time()


doc = load()
meshapps.binary()
joined_at = asyncio.run(join(doc))
meshapps.spawn(PREFIX, doc["nodes"], ["node5"], service=PREFIX, period=1.0, keep_removed=True)
state = meshapps.wait_ready(PREFIX, ["node5"], timeout=RECOVER)["node5"]
left = max(0.0, joined_at + RECOVER - time.time())
ready_at, missing = meshapps.wait_answering(PREFIX, NAMES, since=joined_at, timeout=left)
write_facts({"churn_join": {"joined_at": joined_at, "ready_at": ready_at,
                            "missing": [f"{c}->{p}" for c, p in missing],
                            "app": state["app"]}})
if ready_at is None:
    print(f"driver: the 20 pairs did not answer within {RECOVER:.0f} s of node5's join; "
          f"missing {missing}")
else:
    print(f"driver: the 20 pairs answered {ready_at - joined_at:.1f} s after node5's join")
