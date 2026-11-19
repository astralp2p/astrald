#!/usr/bin/env python3
"""Oracle: a call to an absent app fails as route not found within 1.1 s, and
node1 asks its siblings once for two calls inside the 30 s miss window."""
from lib.sessionio import load

BUDGET_MS = 1100

facts = load()["facts"].get("relay_pull_absent", {})
calls = facts.get("calls") or []
assert len(calls) == 2, f"the driver made {len(calls)} calls; want 2: {facts}"
for i, c in enumerate(calls, 1):
    assert c["route_not_found"], f"call {i} failed with {c['err']!r}; want route not found"
    assert c["ms"] < BUDGET_MS, f"call {i} took {c['ms']} ms; want under {BUDGET_MS}"

tap = facts["tap"]
assert tap["refused"] is None and tap["broken"] is None, f"node1's log tap is incomplete: {tap}"
starts = [a for a in facts["asks"] if a.startswith("asking linked siblings")]
misses = [a for a in facts["asks"] if a.startswith("no relay contract of")]
assert len(starts) == 1, f"node1 asked {len(starts)} times for two calls; want 1: {facts['asks']}"
assert len(misses) == 1, f"node1 recorded {len(misses)} misses; want 1: {facts['asks']}"
print(f"oracle: both calls answered route not found ({calls[0]['ms']} ms, "
      f"{calls[1]['ms']} ms); node1 asked its siblings once")
