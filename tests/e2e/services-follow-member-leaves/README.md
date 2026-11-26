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
  Privileged membership the User signs that expires in 60 s (accepted on the
  member with `user.accept_contract`, offered to node1's SwarmState with
  `nodes.swarm_push`), waits until the follow drops the member after the
  expiry, signs a second membership (1 h), tries `user.accept_contract` with it
  on the member, offers it to node1, and waits until the follow shows the
  member again (`follow_member_leaves`).
- **oracle** `verify.py` — the follow never ended; the offering left with
  cause `member_left`, after the expiry and within 15 s of it; it came back
  within 15 s of node1 accepting the second contract. The answer of
  `user.accept_contract` is printed.

Spec: `.ai/system/protocols/services/ops/services.discover.md` (Following),
`.ai/system/protocols/services/types/services.removed.md`. Tickets:
https://wiki.satforge.dev/doc/6vnVgpjarA,
https://wiki.satforge.dev/doc/0wW504QDxy,
https://wiki.satforge.dev/doc/x4craXsVBd

## Since G7

The memberships are Privileged and signed by the user key: `user.accept_contract`
activates only such a membership (D2), and a lower tier needs a carrier and an
Approval, while `user.join` signs none shorter than an hour. node1 learns
each membership only through SwarmState, so the driver offers it there with
`nodes.swarm_push` instead of `objects.push`. The intent is unchanged: a
follow tracks a member's expiry and its return. Renewal itself stays a
Non-goal; the second membership is a new one.

## Spec note

The plan has the member accept its second contract with
`user.accept_contract`. A running node keeps an expired active contract
until it restarts (`.ai/system/topics/user-and-swarm.md`, Losing the owner),
and astrald answers `user.accept_contract` on a node holding an active
contract with "already claimed". The rejoin that node1's follow sees is the
second contract node1 indexes, so the driver records the op's answer and
judges the follow.
