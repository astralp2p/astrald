#!/usr/bin/env python3
"""Oracle: a node that leaves costs the other apps no hung call, and the
apps that never depended on it keep answering.

The oracle watches WATCH seconds from its start (the event: node4 is
stopped) and judges the apps' records:

- node4 is absent from the session view;
- every remaining app's follow removed node4's offering after the driver's
  end and within 27 s of the event, and does not show it again. The cause is
  `member_lost` (the carry to node4 ended) or `member_removed` (node4
  retracted the offering as it shut down, and a member's removal reaches the
  caller as `member_removed`): astral-docs
  protocols/services/types/services.removed.md:14-25. 27 s is the tcp bound
  N + T + 2 (D11; topics/link-mux.md, Per-transport values);
- every call to node4's app that ended after the driver's end took at most
  27 s, so none ran to the 60 s routing deadline
  (core-definitions/query.md:54-62), every call in flight at the window's
  end started at most 27 s before it, and each remaining app called node4's
  app at least once in the window;
- the 20 pairs among the five remaining apps answered every call started in
  the window, at least MIN_CALLS each: they never depended on node4;
- the oracle stops node4's app.
"""
import time

from lib import apps, meshapps
from lib.sessionio import load

REMAINING = ["node1", "node2", "node3", "node5", "late"]
PREFIX = "f3"
WATCH = 35.0
BOUND = 27.0
MIN_CALLS = 10
CAUSES = ("member_lost", "member_removed")

event = time.time()
doc = load()
f = doc["facts"]["churn_leave"]
app4 = f["app"]
assert "node4" not in doc["nodes"], "node4 is still in the oracle's session view"
time.sleep(max(0.0, event + WATCH - time.time()))
end = time.time()

states = meshapps.states(PREFIX, REMAINING)
logs = {n: meshapps.calls(PREFIX, n) for n in REMAINING}
removals = {}
for c in REMAINING:
    rec = (states[c].get("providers") or {}).get(app4) or {}
    after = [r for r in rec.get("removed") or [] if r["t"] >= f["driver_end"]]
    assert after, f"{c}'s follow never removed node4's offering after node4 stopped: {rec}"
    r = after[0]
    assert r["cause"] in CAUSES, f"{c}'s follow removed node4's offering as {r['cause']}; want {CAUSES}"
    assert r["t"] <= event + BOUND, (
        f"{c}'s follow removed node4's offering {r['t'] - event:.1f} s after the event; "
        f"want ≤ {BOUND:.0f} s")
    assert not rec.get("listed"), f"{c}'s follow shows node4's offering again"
    removals[c] = (round(r["t"] - f["driver_end"], 2), r["cause"])

    to4 = [e for e in logs[c] if e["to"] == app4 and e["t1"] > f["driver_end"]]
    slow = [e for e in to4 if e["t1"] - e["t0"] > BOUND]
    assert not slow, (f"{c}'s app waited more than {BOUND:.0f} s for node4's app: "
                      f"{[(e['outcome'], round(e['t1'] - e['t0'], 1)) for e in slow][:5]}")
    assert meshapps.window(logs[c], app4, event, end), (
        f"{c}'s app made no call to node4's app in the window")
    since = rec.get("inflight_since")
    assert since is None or end - since <= BOUND, (
        f"{c}'s app has waited {end - since:.1f} s for node4's app")

for c, p in meshapps.pairs(REMAINING):
    got = meshapps.window(logs[c], states[p]["app"], event, end)
    bad = [e for e in got if e["outcome"] != "ok"]
    assert not bad, (f"{c}'s app -> {p}'s app failed while node4 was gone: "
                     f"{[(round(e['t0'] - event, 2), e['outcome']) for e in bad][:5]}")
    assert len(got) >= MIN_CALLS, (f"{c}'s app made {len(got)} calls to {p}'s app in the "
                                   f"{WATCH:.0f} s window; want ≥ {MIN_CALLS}")

apps.get(meshapps.app_name(PREFIX, "node4")).stop()
print(f"oracle: node4's offering removed (s after the driver's end, cause) {removals}; no call "
      f"to node4's app waited over {BOUND:.0f} s; the 20 remaining pairs answered every call")
