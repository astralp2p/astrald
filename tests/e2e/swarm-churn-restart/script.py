#!/usr/bin/env python3
"""Driver: the F3 chain's restart step; the harness restarts node2 after it.

The driver records what the oracle compares after the restart and judges
nothing: the `churn_restart` fact, {restarts: {node: count}, attaches (of
node2's app), driver_end}. The harness restarts node2 (SIGINT, then a fresh
start on its state) between this driver and the oracle.
"""
import time

from lib import meshapps
from lib.sessionio import load, write_facts

NAMES = ["node1", "node2", "node3", "node4"]
PREFIX = "f3"

nodes = load()["nodes"]
states = meshapps.states(PREFIX, NAMES)
write_facts({"churn_restart": {
    "restarts": {n: nodes[n]["restarts"] for n in NAMES},
    "attaches": states["node2"].get("attaches", 0),
    "driver_end": time.time()}})
print(f"driver: node2's app attached {states['node2'].get('attaches', 0)} time(s); "
      "the harness restarts node2 next")
