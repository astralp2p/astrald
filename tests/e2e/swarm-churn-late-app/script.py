#!/usr/bin/env python3
"""Driver: the F3 chain's late-app step. The harness stops node2 after it,
runs while_down.py, which registers a sixth app on node1, and starts node2.

The driver judges nothing and records the `churn_late_app` fact: {restarts:
{node: count}, driver_end}.
"""
import time

from lib.sessionio import load, write_facts

NAMES = ["node1", "node2", "node3", "node4", "node5"]

nodes = load()["nodes"]
write_facts({"churn_late_app": {"restarts": {n: nodes[n]["restarts"] for n in NAMES},
                                "driver_end": time.time()}})
print("driver: the harness stops node2 next; while_down.py registers the late app")
