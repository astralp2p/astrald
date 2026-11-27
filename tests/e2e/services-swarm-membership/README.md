# services-swarm-membership

An open `reach=swarm` follow on node1 tracks the swarm member set: a node
adopted after the follow opened contributes its offering, and the offering is
removed when the node is expelled. A node admitted with a membership contract
that expires after 90 seconds contributes its offering until the expiry, and
the offering is removed within 15 seconds after it. The follow is never
reopened.

- **env** node · **start** `two-nodes` · **destroys** — `ssm_late` is expelled.
- **driver** `script.py` — links the fresh nodes `ssm_late` and `ssm_expiry`
  to node1 and reads `ssm_expiry`'s node key through `lib/nodefs.py`, then
  runs `services-swarm/driver` in `membership` mode: a follow on node1, the
  join of `ssm_late` through node1 as a Member, its expulsion, and
  `ssm_expiry`'s short membership. The driver signs `ssm_expiry`'s
  PrivilegedMembership with the User's key on node1 and with the node key, and
  activates it with `user.accept_contract` on `ssm_expiry`. SwarmState carries
  it to node1. Each provider app registers on its node after that node's join
  or claim.
- **oracle** `verify.py` — all four steps held.

Spec: `.ai/system/protocols/services/ops/services.discover.md`,
`.ai/system/protocols/user/types/mod.user.swarm_member_left.md`.

Changed in G7, for the removed model:

- The providers register after the join or the claim. An app's contract signed
  while its node was unclaimed does not outlive the claim, which is a Non-goal
  ("contracts signed before a claim").
- `ssm_expiry`'s membership is a PrivilegedMembership. Since G7 the user key
  signs no other membership: a carrier signs a Member's for an Approval.
- The contract is no longer pushed to node1, because node1 refuses a pushed
  membership. The claim adds it to `ssm_expiry`'s SwarmState. mod/user then
  exchanges SwarmState with each peer `ssm_expiry` is linked to
  (`exchangeSwarmWithLinked`, `mod/user/src/swarm_exchange.go`).
