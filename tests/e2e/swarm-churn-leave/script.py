#!/usr/bin/env python3
"""Driver: the F3 chain's leave step; the harness stops node4 after it.

The driver judges nothing and records the `churn_leave` fact: {driver_end,
app (node4's app id)}. The harness stops node4 for good (SIGINT) between
this driver and the oracle; node4's app keeps running until the oracle stops
it.
"""
import time

from lib import meshapps
from lib.sessionio import write_facts

PREFIX = "f3"

app = meshapps.states(PREFIX, ["node4"])["node4"].get("app")
write_facts({"churn_leave": {"driver_end": time.time(), "app": app}})
print("driver: the harness stops node4 next")
