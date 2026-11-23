# services-follow-member-leaves

An open `reach=swarm` follow removes a member's offering when the member's
membership expires, and shows it again, without reopening, when the member
rejoins with a new contract. This is M13 of the E2E tests exhibit
(https://wiki.satforge.dev/doc/e2e-tests-X3LHjItA2b). The expelled case is
`services-swarm-membership`.

- **after** `adopt-node` · **destroys** `true` — sfml_member joins the
  User's swarm.
- **driver** `script.py` with `services-swarm/driver rejoin` — links
  sfml_member to node1 and reads its node key (`lib/nodefs.py`). The Go
  program opens a follow of `sfml` on node1 (one stream, no reconnect),
  serves a provider of `sfml` on sfml_member, admits sfml_member with a
  contract that expires in 60 s (accepted on the member, pushed to node1),
  waits until the follow drops the member after the expiry, signs a second
  contract (1 h), tries `user.accept_contract` with it on the member, pushes
  it to node1, and waits until the follow shows the member again
  (`follow_member_leaves`).
- **oracle** `verify.py` — the follow never ended; the offering left with
  cause `member_left`, after the expiry and within 15 s of it; it came back
  within 15 s of node1 accepting the second contract. The answer of
  `user.accept_contract` is printed.

Spec: `.ai/system/protocols/services/ops/services.discover.md` (Following),
`.ai/system/protocols/services/types/services.removed.md`. Tickets:
https://wiki.satforge.dev/doc/6vnVgpjarA,
https://wiki.satforge.dev/doc/0wW504QDxy,
https://wiki.satforge.dev/doc/x4craXsVBd

## Spec note

The plan has the member accept its second contract with
`user.accept_contract`. A running node keeps an expired active contract
until it restarts (`.ai/system/topics/user-and-swarm.md`, Losing the owner),
and astrald answers `user.accept_contract` on a node holding an active
contract with "already claimed". The rejoin that node1's follow sees is the
second contract node1 indexes, so the driver records the op's answer and
judges the follow.
