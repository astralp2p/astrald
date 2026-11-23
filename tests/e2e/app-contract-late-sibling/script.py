#!/usr/bin/env python3
"""Driver: record what the oracle compares node2's restart against.

The flow runs around the restart: the runner stops node2 after this driver,
while_down.py registers a provider app on node1, and the runner starts node2
again before the oracle. The driver records the `late_sibling` fact:
{node1_restarts_before, node2_restarts_before, node2_endpoint, driver_end}.
It judges nothing.
"""
import time

from lib.sessionio import load, write_facts

nodes = load()["nodes"]
write_facts({"late_sibling": {
    "node1_restarts_before": nodes["node1"].get("restarts") or 0,
    "node2_restarts_before": nodes["node2"].get("restarts") or 0,
    "node2_endpoint": nodes["node2"]["endpoint"],
    "driver_end": time.time(),
}})
print("driver: recorded both nodes' restarts; node2 restarts next")
