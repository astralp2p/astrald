# services-carry-eos-retry

A swarm follow whose carry to a member ends five times over, each time after
the member delivered its initial outcome, restores the member's offering each
time within the first backoff step: the carry's retry delay resets after each
delivered boundary. This is M12 of the E2E tests exhibit
(https://wiki.satforge.dev/doc/e2e-tests-X3LHjItA2b).

- **after** `adopt-node` · **destroys** `true` — five extra links stay ·
  **host_listener** — the two mesh apps serve on the test host.
- **driver** `script.py` — spawns the `m12` mesh apps (period 0): the provider
  on node2, the follower on node1. The User opens five more links node1 →
  node2. Five cycles: the driver finds node1's outbound `services.discover`
  session to node2 (`nodes.sessions`), closes its link (`nodes.close_link`),
  waits up to 30 s until the follower recorded the removal and the restore,
  and pauses 2 s (`carry_retry`).
- **oracle** `verify.py` — five cycles ran and the follow opened once; no link
  appeared during a cycle and one remained; each removal is `member_lost`
  within 3 s of the close, and each restore arrives within 4 s of the removal.

Spec: `.ai/system/protocols/services/types/services.removed.md`
(member_lost, backoff 1 s to 60 s). Code: `mod/services/src/swarm.go`
(`carry`: the delay resets once the member's initial outcome arrived).
Tickets: https://wiki.satforge.dev/doc/dDWBgaaAeg,
https://wiki.satforge.dev/doc/GjfZ1boOdd,
https://wiki.satforge.dev/doc/cXSPe6unaV

## Spec conflict

The exhibit says a carry stream that ends without EOS still resets the retry
delay. astrald resets it only when the member's initial outcome (the boundary
EOS) arrived, and the spec states the backoff but not its reset. Every cycle
here closes a carry that delivered its boundary, so the test judges the
reset astrald implements.
