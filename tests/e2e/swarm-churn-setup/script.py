#!/usr/bin/env python3
"""Driver: the first step of the F3 churn chain spawns the `f3` mesh apps.

One app per node, node1-node4 (`lib/meshapps.py`: service `f3`, period 1 s,
`keep_removed`, so an app keeps calling a provider its follow removed). The
apps outlive this driver and serve every later step of the chain
(tests/README.md, Long-lived apps). The driver waits up to 60 s until all 12
pairs answer.

It judges nothing and records the `churn_setup` fact: {apps: {node: app id},
pids: {node: pid}, spawned_at, ready_at (null when the 12 pairs did not
answer within 60 s), missing}.
"""
import time

from lib import meshapps
from lib.sessionio import load, write_facts

NAMES = ["node1", "node2", "node3", "node4"]
PREFIX = "f3"
# why 60 s: the per-step recovery bound of the F3 flow (D11)
READY = 60.0

nodes = load()["nodes"]
meshapps.binary()
spawned_at = time.time()
meshapps.spawn(PREFIX, nodes, NAMES, service=PREFIX, period=1.0, keep_removed=True)
states = meshapps.wait_ready(PREFIX, NAMES)
ready_at, missing = meshapps.wait_answering(PREFIX, NAMES, since=spawned_at, timeout=READY)
write_facts({"churn_setup": {
    "apps": meshapps.ids(states),
    "pids": {n: s["pid"] for n, s in states.items()},
    "spawned_at": spawned_at, "ready_at": ready_at,
    "missing": [f"{c}->{p}" for c, p in missing]}})
if ready_at is None:
    print(f"driver: the 12 pairs did not answer within {READY:.0f} s; missing {missing}")
else:
    print(f"driver: the 12 pairs answered {ready_at - spawned_at:.1f} s after the spawn")
