# swarm-state-join-carrier-restart

Criterion 2.1, step 2 of 3: the restarted joiner asks again, then the
carrier restarts.

- **after** `swarm-state-join-unanswered` · **restart** `node1` (the
  carrier) · **destroys** `true`.
- **driver** `script.py` — node3, restarted by step 1, runs `user.join`
  through node1 again at tier `privileged_member` and gives up after 10 s
  with the User confirming nothing. Records `join_resent`: node3's
  JoinRequests node1 lists after the resend.
- **oracle** `verify.py` — after the resend node1 listed exactly one
  JoinRequest of node3, the one step 1 recorded, pending (the joiner kept it
  across its restart and resent the same object); after node1's restart it
  still lists that one request, pending.

**Strictness.** With the joiner keeping no request across its restart
(`newSwarmJoiner` given no kept request), the oracle fails: node1 lists two
JoinRequests of node3. The carrier keeps the request in its SwarmState copy
(the node VFS), which step 3 reads after node1's restart.
