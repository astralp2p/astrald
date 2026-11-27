# swarm-state-join-resumed

Criterion 2.1, step 3 of 3: the User confirms the join within the
JoinRequest's hour, after both restarts, and node3 is admitted.

- **after** `swarm-state-join-carrier-restart`.
- **driver** `script.py` — the User, on node1, confirms node3's pending
  `privileged_member` JoinRequest (`user.confirm_join`) while node3 runs no
  join, so the confirmation comes while the two sides are apart; node3 then
  runs `user.join` through node1 once more, resending the same request, and
  co-signs the membership in the carrier session. Records `join_resumed`:
  what `user.join` answered, the confirmed request and its Timeout, and when.
- **oracle** `verify.py` — the confirmed request is the one node3 filed in
  step 1, before both restarts, confirmed before its Timeout; `user.join`
  answered node3's membership, issued by the user key at tier
  `privileged_member`, naming that request as its `RequestID`; node1 lists
  the request fulfilled; node3 holds the membership as its active contract
  and sees the swarm as a member.

**Strictness.** Step 3 asserts that the confirmed request is the one filed
before both restarts; step 2 fails first when the joiner does not keep it
(see its README). node1 keeps a confirmation in memory until its carrier
hands it over, so the User confirms after node1's restart, not before it.
