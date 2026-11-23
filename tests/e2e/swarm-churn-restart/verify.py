#!/usr/bin/env python3
"""Oracle: after node2 restarts, every pair of the four mesh apps answers
again within 60 s, and node2's app re-registered in the same process.

- node2 restarted once, the other nodes not (the session view's `restarts`);
- the event time is the oracle's start, once node2 runs again; within 60 s of
  it every pair answers since the event (D11: 60 s per churn step; the
  callers retry an unavailable host, astral-docs
  core-definitions/query.md:68-71, and a reconnecting follow restores the
  restarted member's offering, protocols/services/ops/services.discover.md,
  Following);
- node2's app is the process `swarm-churn-setup` spawned, and its handler
  attached again (`attaches` grew): an app re-registers its handler with the
  node that restarted (topics/external-providers.md:45-46);
- every app still runs.

The recovery time is printed; `swarm-survives-churn` tabulates the chain.
"""
import time

from lib import apps, meshapps
from lib.sessionio import load

NAMES = ["node1", "node2", "node3", "node4"]
PREFIX = "f3"
RECOVER = 60.0

event = time.time()
doc = load()
nodes, facts = doc["nodes"], doc["facts"]
f = facts["churn_restart"]
want = {n: f["restarts"][n] + (1 if n == "node2" else 0) for n in NAMES}
got = {n: nodes[n]["restarts"] for n in NAMES}
assert got == want, f"restart counts {got}; want {want} (node2 once, the others not)"

at, missing = meshapps.wait_answering(PREFIX, NAMES, since=event, timeout=RECOVER)
assert at is not None, (f"pairs not answering within {RECOVER:.0f} s of node2's restart: "
                        f"{missing}")
took = at - event
assert took <= RECOVER, f"every pair answered {took:.1f} s after the restart; want ≤ {RECOVER:.0f} s"

st = meshapps.states(PREFIX, ["node2"])["node2"]
pid = facts["churn_setup"]["pids"]["node2"]
assert st.get("pid") == pid, f"node2's app runs as pid {st.get('pid')}; want the spawned {pid}"
assert st.get("attaches", 0) > f["attaches"], (
    f"node2's app attached {st.get('attaches')} time(s), {f['attaches']} before the restart: "
    "its handler never re-registered")
dead = [n for n in NAMES if not apps.get(meshapps.app_name(PREFIX, n)).alive()]
assert not dead, f"the f3 apps of {dead} no longer run"

print(f"oracle: recovery after node2's restart {took:.1f} s; node2's app re-registered "
      f"({st['attaches']} attaches) in pid {pid}")
