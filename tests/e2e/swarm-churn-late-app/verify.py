#!/usr/bin/env python3
"""Oracle: an app that registered while a sibling was down is reached by that
sibling's app on its first call after the sibling returns.

- node2 restarted once, the other nodes not;
- the event time is the oracle's start, once node2 runs again; within 60 s
  every pair of the six apps answers since then (D11);
- node2's app's first call to the late app answered `ok`, not
  `route_not_found`: a member not linked when an app registers receives its
  contract at its next first link to a member that holds it, and a member
  that missed it pulls it when it routes (astral-docs
  topics/app-routing.md:164-169, Reach);
- every app still runs.
"""
import time

from lib import apps, meshapps
from lib.sessionio import load

NAMES = ["node1", "node2", "node3", "node4", "node5", "late"]
NODES = ["node1", "node2", "node3", "node4", "node5"]
PREFIX = "f3"
RECOVER = 60.0

event = time.time()
doc = load()
f = doc["facts"]["churn_late_app"]
want = {n: f["restarts"][n] + (1 if n == "node2" else 0) for n in NODES}
got = {n: doc["nodes"][n]["restarts"] for n in NODES}
assert got == want, f"restart counts {got}; want {want} (node2 once, the others not)"

states = meshapps.states(PREFIX, NAMES)
late = states["late"].get("app")
assert late, "the late app wrote no state: while_down.py never registered it"
at, missing = meshapps.wait_answering(PREFIX, NAMES, since=event, timeout=RECOVER)
assert at is not None, (f"pairs not answering within {RECOVER:.0f} s of node2's return: "
                        f"{missing}")
took = at - event

states = meshapps.states(PREFIX, NAMES)
first = (meshapps.peer(states, "node2", "late") or {}).get("first") or {}
assert first.get("outcome") == "ok", (
    f"node2's app's first call to the late app ended {first.get('outcome')} "
    f"({first.get('err')}); want ok on the first attempt")
dead = [n for n in NAMES if not apps.get(meshapps.app_name(PREFIX, n)).alive()]
assert not dead, f"the f3 apps of {dead} no longer run"

print(f"oracle: recovery after node2's return {took:.1f} s for the 30 pairs; node2's app's "
      f"first call to the late app {late[:12]}… answered ok in {first['ms']:.0f} ms")
