#!/usr/bin/env python3
"""Oracle: a frozen peer reads as a timeout, a dead link as unavailable.

The driver records how each query to node2 ended and when. The oracle reads
those facts; it repeats nothing.
"""
from lib.sessionio import load

DEADLINE_MS = 60_000  # astrald's routing deadline (core/router.go)

facts = load()["facts"].get("dead_peer", {})

assert facts.get("node2_knows_the_caller") is True, "node2 never held the caller's relay contract"


def ended(name):
    o = facts.get(name)
    assert o, f"no outcome recorded for {name}: {sorted(facts)}"
    return o


live = ended("live")
assert live["kind"] == "route_not_found", f"live: ended {live['kind']!r} ({live['error']!r}); want route_not_found"

frozen = ended("frozen")
assert frozen["kind"] == "timeout", (
    f"frozen: ended {frozen['kind']!r} ({frozen['error']!r}) after {frozen['millis']} ms; want timeout")
assert DEADLINE_MS - 1000 <= frozen["millis"] <= DEADLINE_MS + 5000, (
    f"frozen: timed out after {frozen['millis']} ms; want about {DEADLINE_MS} ms")
assert frozen["retryable"] is True, "frozen: IsRetryable said false for a timeout"

killed = ended("killed")
assert (killed["kind"], killed["code"]) == ("rejected", 7), (
    f"killed: ended {killed['kind']!r} {killed['code']} ({killed['error']!r}); want rejected 7")
after = facts.get("answered_after_kill_millis")
assert after is not None and after < 1000, f"killed: answered {after} ms after node2 died; want within 1000 ms"
assert killed["retryable"] is True, "killed: IsRetryable said false for unavailable"

print("oracle: the frozen peer timed out at the deadline; the killed peer's link answered unavailable at once")
