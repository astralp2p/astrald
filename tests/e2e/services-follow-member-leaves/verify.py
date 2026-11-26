#!/usr/bin/env python3
"""Oracle: an open `reach=swarm` follow removes a member's offering when its
membership expires, and shows it again, without reopening, when the member
rejoins with a new contract.

From the `follow_member_leaves` fact:

- the flow ran to its end, and the one follow it opened never ended;
- the member's offering left the follow with cause `member_left`, after the
  contract's expiry and within 15 s of it (astral-docs
  protocols/services/ops/services.discover.md:173-180, Following: a member
  leaves when its last membership contract expires;
  protocols/services/types/services.removed.md:21-22; 15 s as
  `services-swarm-membership`: node1 republishes its member set 1 s after
  the expiry, mod/user/src/swarm_members.go:21-23);
- the offering came back as an update within 15 s of node1 accepting the
  second contract (services.discover.md:170-172: a node that joins the swarm
  becomes a contributing member; node1 stores an entry offered by
  swarm_push in SwarmState before it answers Ack,
  mod/nodes/src/swarm_sync.go:187-211).

`user.accept_contract` on the member is tried with the second contract and
its answer is printed, not judged: a running node keeps an expired contract
until it restarts (topics/user-and-swarm.md:431-432), and astrald refuses the
op on a node that holds one (mod/user/src/op_accept_contract.go:39-43).
"""
from lib.sessionio import load

LEAVE = 15.0
BACK = 15.0

f = load()["facts"]["follow_member_leaves"]
assert not f["err"], f"the flow stopped: {f['err']}"
assert not f["follow_ended"], f"the follow ended during the flow: {f['follow_ended']}"
assert f["removed_cause"] == "member_left", (
    f"the member's offering left the follow with cause {f['removed_cause']!r}; want member_left")
left = f["removed_at"] - f["expires_at"]
assert left >= 0, f"the follow dropped the member {-left:.1f} s before its contract expired"
assert left <= LEAVE, (f"the follow dropped the member {left:.1f} s after its contract expired; "
                       f"want ≤ {LEAVE:.0f} s")
back = f["back_at"] - f["pushed_at"]
assert back <= BACK, (f"the follow showed the rejoined member {back:.1f} s after node1 took "
                      f"its second contract; want ≤ {BACK:.0f} s")

print(f"oracle: member_left {left:.1f} s after the expiry; back {back:.1f} s after the second "
      f"contract; user.accept_contract on the lapsed member answered {f['accept_second']!r}")
