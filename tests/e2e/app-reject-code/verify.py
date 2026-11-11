#!/usr/bin/env python3
"""Oracle: the provider's reject code reached both callers unchanged.

The driver records, per caller, the error the query ended with: a rejection
and its code, or the error text. The oracle reads those facts; it repeats
nothing.
"""
from lib.sessionio import load

WANT = 7  # the code the provider rejects with

facts = load()["facts"].get("app_reject_code", {})

for step in ("provider_serves_on_node2", "node1_knows_the_provider"):
    assert facts.get(step), f"step did not hold: {step}"

for caller in ("local", "remote"):
    where = "node2 (same node)" if caller == "local" else "node1 (across the link)"
    err = facts.get(f"{caller}_error")
    assert err is not None, f"the caller on {where} recorded no outcome: {sorted(facts)}"
    assert facts.get(f"{caller}_rejected") is True, (
        f"the caller on {where} was not rejected; its query ended with {err!r}"
    )
    code = facts.get(f"{caller}_code")
    assert code == WANT, f"the caller on {where} read reject code {code}; want {WANT}"

print("oracle: reject code 7 reached the caller on node2 and the caller on node1 unchanged")
