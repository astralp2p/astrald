#!/usr/bin/env python3
"""Oracle: a provider whose host was expelled reads as a final answer.

The driver records how a call to the provider ended before and after the User
expelled its host. The oracle reads those facts; it repeats nothing.
"""
from lib.sessionio import load

facts = load()["facts"].get("expelled_host", {})

for step in ("node1_knows_the_provider", "node2_knows_the_caller", "expelled"):
    assert facts.get(step) is True, f"step did not hold: {step}"

before = facts["before"]
assert before["kind"] == "accepted", f"before: the call ended {before['kind']!r} ({before['error']!r})"

# why route_not_found: a claimed node counts only current swarm members as
# hosts of an app (astral-docs topics/app-routing.md, Resolving the host), so
# node1 has no host left for the provider once node2 is expelled.
after = facts["after"]
got = f"{after['kind']} {after['code']}" if after["kind"] == "rejected" else after["kind"]
assert got == "route_not_found", f"after: the call ended {got!r} ({after['error']!r}) in {after['millis']} ms; want 'route_not_found'"
assert after["retryable"] is False, "after: IsRetryable said true for an expelled host"

print("oracle: after its host was expelled, the provider reads as route not found and not retryable")
