#!/usr/bin/env python3
"""Oracle: the open follow gained the late member's offering and lost it on
expulsion, and lost a short-contract member's offering at the contract's expiry."""
from lib.sessionio import load

STEPS = ["late_join_reaches_open_follow", "expulsion_removes_from_open_follow",
         "short_member_reaches_open_follow", "expiry_removes_from_open_follow"]

facts = load()["facts"].get("services_swarm_membership", {})
missing = [s for s in STEPS if not facts.get(s)]
assert not missing, f"steps that did not hold: {missing}"
print("oracle: the open swarm follow tracked the member set")
