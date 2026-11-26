#!/usr/bin/env python3
"""Oracle: a node that joins with its own app is reached, and reaches every
member's app, within 60 s, on the first attempt.

- all 20 pairs of the five apps answered within 60 s of node5's join
  (D11), read again live from the apps' state files;
- every first call to and from node5's app answered `ok`: a joined member
  contributes to the open follows (astral-docs
  protocols/services/ops/services.discover.md:168-172, Following), and a new
  member learns the swarm's app contracts from the first member it links to
  (topics/app-routing.md:159-163, Reach);
- every app still runs.
"""
from lib import apps, meshapps
from lib.sessionio import load

NAMES = ["node1", "node2", "node3", "node4", "node5"]
PREFIX = "f3"
RECOVER = 60.0

f = load()["facts"]["churn_join"]
assert f["ready_at"] is not None, (
    f"the 20 pairs did not answer within {RECOVER:.0f} s of node5's join; "
    f"missing {f['missing']}")
took = f["ready_at"] - f["joined_at"]
assert took <= RECOVER, f"the 20 pairs answered {took:.1f} s after the join; want ≤ {RECOVER:.0f} s"

states = meshapps.states(PREFIX, NAMES)
missing = meshapps.answering_since(states, NAMES, f["joined_at"])
assert not missing, f"pairs not answering since node5's join, read live: {missing}"
firsts = meshapps.first_calls(states, NAMES)
bad = {f"{c}->{p}": (call or {}).get("outcome") for (c, p), call in firsts.items()
       if "node5" in (c, p) and (call or {}).get("outcome") != "ok"}
assert not bad, f"first calls to or from node5's app that did not answer ok: {bad}"
dead = [n for n in NAMES if not apps.get(meshapps.app_name(PREFIX, n)).alive()]
assert not dead, f"the f3 apps of {dead} no longer run"

print(f"oracle: recovery after node5's join {took:.1f} s; every first call to and from "
      "node5's app ok")
