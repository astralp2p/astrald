# swarm-state-join-resumed

Criterion 2.1, step 3 of 3: the User says yes within the JoinRequest's hour,
after both restarts, and node3 is admitted.

- **after** `swarm-state-join-carrier-restart`.
- **driver** `script.py` — node3 runs `user.join` through node1 once more
  while the User, on node1, approves node3's pending JoinRequest
  (`user.answer_request`). Records `join_resumed`: what `user.join`
  answered, the approved request and its Timeout, and when.
- **oracle** `verify.py` — the approved request is the one node3 filed in
  step 1, before both restarts, approved before its Timeout; `user.join`
  answered node3's membership, issued by node1 at tier member with an
  ApprovalID; node1 lists the request approved; node3 holds the membership
  as its active contract and sees the swarm as a member.

**Strictness.** Step 3 asserts that the approved request is the one filed
before both restarts; step 2 fails first when the joiner does not keep it
(see its README).
