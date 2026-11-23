#!/usr/bin/env python3
"""Oracle: four mesh apps of one swarm reach each other at once.

- all 12 pairs answered within 60 s of the spawn (D11: the F3 flow's 60 s
  bound per step), read again live from the apps' state files;
- every pair's first call answered `ok`: a swarm member routes a call to an
  app hosted on another member on the first attempt (astral-docs
  topics/app-routing.md:157-168, Reach: a node gives the members it is linked
  to the contract of an app it hosts when the app registers, and a member
  that missed it pulls it on the miss);
- every app still runs: the rest of the chain depends on them.
"""
from lib import apps, meshapps
from lib.sessionio import load

NAMES = ["node1", "node2", "node3", "node4"]
PREFIX = "f3"
READY = 60.0

f = load()["facts"]["churn_setup"]
assert f["ready_at"] is not None, (
    f"the 12 pairs did not all answer within {READY:.0f} s of the spawn; missing {f['missing']}")
took = f["ready_at"] - f["spawned_at"]
assert took <= READY, f"the 12 pairs answered {took:.1f} s after the spawn; want ≤ {READY:.0f} s"

states = meshapps.states(PREFIX, NAMES)
missing = meshapps.answering_since(states, NAMES, f["spawned_at"])
assert not missing, f"pairs not answering since the spawn, read live: {missing}"
firsts = meshapps.first_calls(states, NAMES)
bad = {f"{c}->{p}": (call or {}).get("outcome") for (c, p), call in firsts.items()
       if (call or {}).get("outcome") != "ok"}
assert not bad, f"first calls that did not answer ok: {bad}"
dead = [n for n in NAMES if not apps.get(meshapps.app_name(PREFIX, n)).alive()]
assert not dead, f"the f3 apps of {dead} no longer run"

print(f"oracle: the 12 pairs answered {took:.1f} s after the spawn, every first call ok")
