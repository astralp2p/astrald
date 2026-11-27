# swarm-state-join-unanswered

Criterion 2.1, step 1 of 3: a node asks to join while nobody answers, and
the joiner restarts. Steps 2 and 3 are `swarm-state-join-carrier-restart`
and `swarm-state-join-resumed`.

- **after** `adopt-node` · **restart** `node3` (the joiner) · **destroys**
  `true` — node3 holds a pending join through node1.
- **driver** `script.py` — node1 and node3 learn each other's endpoint; node3
  runs `user.join` through node1 at tier `privileged_member` and gives up
  after 10 s with the User confirming nothing. Records `join_unanswered`: the
  JoinRequest's entry ID and filing time, as node1 lists it, and what
  `user.join` answered.
- **oracle** `verify.py` — after node3's restart: `user.join` answered
  nothing, node1 lists exactly one JoinRequest of node3, the recorded one,
  pending, and node3 is not claimed.

**Why `privileged_member`.** A carrier signs a `member` or `trusted`
membership in the carrier session at once, so only a `privileged_member`
join waits for the User: node1 holds the software user key, and its carrier
signs nothing until the User confirms with `user.confirm_join`
(astral-docs `protocols/user/README.md#joins`).
