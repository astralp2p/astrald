#!/usr/bin/env python3
"""Oracle: each failing query ended with the answer of its own cause.

The driver records, per query, how it ended (accepted, rejected with a code,
or an apphost error code), whether IsRetryable called it retryable, and how
long it took. The oracle reads those facts; it repeats nothing.
"""
from lib.sessionio import load

# An answer that waits for nothing. The criterion is 100 ms on an idle node;
# why 500: in the shared main.suite world on a loaded 4-core host node1's own
# core.Router logged 133-168 ms for these queries (2026-11-18), and 500 ms still
# tells an immediate answer from the 60 s routing deadline.
FAST_MS = 500

facts = load()["facts"].get("route_outcomes", {})

for step in ("node2_knows_the_caller", "provider_listed", "node1_knows_the_provider",
             "provider_still_listed", "caller_relay_withdrawn"):
    assert facts.get(step) is True, f"step did not hold: {step}"


def outcome(name, kind, code=None, fast=True, retryable=False):
    o = facts.get(name)
    assert o, f"no outcome recorded for {name}: {sorted(facts)}"
    got = o["kind"] if code is None else f'{o["kind"]} {o["code"]}'
    want = kind if code is None else f"{kind} {code}"
    assert got == want, f"{name}: the query ended {got!r} ({o['error']!r}); want {want!r}"
    assert o["retryable"] is retryable, f"{name}: IsRetryable said {o['retryable']}; want {retryable}"
    if fast:
        assert o["millis"] < FAST_MS, f"{name}: answered in {o['millis']} ms; want under {FAST_MS} ms"


outcome("unknown_identity", "route_not_found")
outcome("no_such_op", "route_not_found")
outcome("refused", "rejected", 1)
outcome("filtered", "target_not_allowed")
outcome("provider_served", "accepted", fast=False)
outcome("handler_gone", "route_not_found")
outcome("relay_refused", "rejected", 1)

print("oracle: no route, refusal, relay refusal, policy block and a gone handler each read as their own outcome")
