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
  adoption of `ssm_late`, its expulsion, and `ssm_expiry`'s short membership.
  The driver signs `ssm_expiry`'s contract with the User's key on node1 and
  with the node key, activates it with `user.accept_contract` on
  `ssm_expiry`, and pushes it to node1.
- **oracle** `verify.py` — all four steps held.

Spec: `.ai/system/protocols/services/ops/services.discover.md`,
`.ai/system/protocols/user/types/mod.user.swarm_member_left.md`.
