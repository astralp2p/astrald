#!/usr/bin/env python3
"""Oracle: a swarm follow whose carry to a member ends five times over, each
time after the member delivered its initial outcome, restores the member's
offering each time within the first backoff step.

From the `carry_retry` fact and the follower's state file:

- the flow ran five cycles, and the follower's follow opened once: the
  removals and restores are the node's, not a reopened follow's;
- precondition: no link to node2 appeared during a cycle, and node1 kept a
  link to node2 after each close. A new link wakes the carry at once
  (astrald mod/services/src/swarm.go:67-90, carry), and would prove nothing;
- each cycle: the follower recorded the removal of node2's provider with
  cause `member_lost` within 3 s of the close (astral-docs
  protocols/services/types/services.removed.md:14-18: the carry to the
  contributing member ended);
- each cycle: the offering came back as `services.update` within 4 s of the
  removal. The spec states the backoff, 1 s to 60 s (services.removed.md
  member_lost), and not its reset (plan conflicts C2, C12); astrald resets
  the delay once the member's initial outcome arrived
  (mod/services/src/swarm.go:83-86), so every cycle waits the first step,
  1 s. Without the reset the fifth cycle waits 16 s.
"""
from lib import meshapps
from lib.sessionio import load

PREFIX = "m12"
CYCLES = 5
REMOVED = 3.0
RESTORED = 4.0

f = load()["facts"]["carry_retry"]
assert f["error"] is None, f"the flow stopped after {len(f['cycles'])} cycles: {f['error']}"
assert len(f["cycles"]) == CYCLES, f"{len(f['cycles'])} cycles ran; want {CYCLES}"
follower = meshapps.states(PREFIX, ["node1"])["node1"]
assert follower.get("follow_opens") == 1, (
    f"the follower's follow opened {follower.get('follow_opens')} times; want once")

rows = []
for i, c in enumerate(f["cycles"], 1):
    new = sorted(set(c["links_after"]) - set(c["links_before"]))
    assert not new, f"cycle {i}: link(s) {new} to node2 appeared; a new link wakes the carry"
    assert c["links_after"], f"cycle {i}: node1 holds no link to node2 after the close"
    r = c["removal"]
    assert r is not None and c["restored_at"] is not None, (
        f"cycle {i}: the follower recorded no removal and restore within 30 s of closing "
        f"link(s) {c['closed']}")
    gone = r["t"] - c["closed_at"]
    assert r["cause"] == "member_lost", f"cycle {i}: removal cause {r['cause']}; want member_lost"
    assert gone <= REMOVED, (f"cycle {i}: removal {gone:.2f} s after the close; "
                             f"want ≤ {REMOVED:.0f} s")
    back = c["restored_at"] - r["t"]
    assert back <= RESTORED, (f"cycle {i}: restore {back:.2f} s after the removal; want ≤ "
                              f"{RESTORED:.0f} s (the retry delay did not reset)")
    rows.append((round(gone, 2), round(back, 2)))

print(f"oracle: five carry losses, each (removal after close, restore after removal) s: {rows}")
