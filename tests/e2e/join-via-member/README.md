# join-via-member

A node asks to join a User's swarm as a Privileged member through a member
that does not hold the User's key, and is refused without a membership;
through the member that does, and is admitted, once the User confirms, into a
roster every node agrees on.

- **after** `adopt-node` (node1 holds the User's key and a Privileged
  membership; node2 is a Privileged member holding only its membership) · **nodes** node1, node2 and
  `jvm-latecomer`, a fresh node no other test uses · **destroys** `true` — the
  swarm others rely on now holds a third node, and `swarm-roster-sync` and
  `swarm-member-restart` compare exact rosters, so no test may follow this one
  in its world. It sits after `swarm-member-restart` in `main.suite` so every
  sibling keeps the shared world, and the planner replays `bootstrap-user-
  software-key → adopt-node` for it in a world of its own.
- **driver** `script.py` — the latecomer learns node1's and node2's
  endpoints, node1 learns the latecomer's, node2 is told nothing. The
  latecomer calls `user.join` on node2 at `tier=privileged_member`, and the
  driver records the answer. After a 3 s pause, so a membership spreading
  through SwarmState would have shown, it records the latecomer's `user.info`
  state and node1's and node2's `user.swarm_status`. Then the latecomer calls
  the same `user.join` on node1, while the User confirms the JoinRequest there
  with `user.confirm_join`.
- **oracle** `verify.py` — the join through node2 was refused with "a
  privileged membership needs the user key's signature" (only the user key
  signs a Privileged membership, D2, and node2 holds no copy of it;
  `mod/user/src/swarm_carrier.go` `errUserKeyRequired`); after it the
  latecomer was `fresh` and node1's and node2's rosters held exactly node1
  and node2; the join through node1 was admitted with a membership issued by
  the User to the latecomer. On each node's own token, within 30 s, the
  latecomer is `claimed` by the same User, and `user.swarm_status` lists
  exactly node1, node2 and the latecomer, with `user.list_siblings` and the
  linked members exactly the other two.

`user.join` is the latecomer's request: it signs a JoinRequest naming the
carrier and sends it to the carrier's `user.carry_join`. The carrier writes the
request into SwarmState (D9 keeps every request) and, for a Privileged tier,
waits for the user key to sign the membership after the User's own action.
node2 cannot reach the user key, so it ends the join at once, and no
membership is signed, stored or handed over: the oracle holds "no debris" to a
fresh latecomer and unchanged rosters. A Member join through node2 would be
admitted on an Approval, since a Privileged carrier signs a Member's
membership itself; that path is `swarm-state-member`'s.

The test holds that only the User's key brings in a node with the User's
authority.
