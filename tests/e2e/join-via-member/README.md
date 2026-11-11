# join-via-member

A node joins a User's swarm through a member that does not hold the User's
key, and is refused without a trace; through the member that does, and is
admitted into a roster every node agrees on.

- **after** `adopt-node` (node1 holds the User's key and a contract; node2 is
  a member holding only its contract) · **nodes** node1, node2 and
  `jvm-latecomer`, a fresh node no other test uses · **destroys** `true` — the
  swarm others rely on now holds a third node, and `swarm-roster-sync` and
  `swarm-member-restart` compare exact rosters, so no test may follow this one
  in its world. It sits after `swarm-member-restart` in `main.suite` so every
  sibling keeps the shared world, and the planner replays `bootstrap-user-
  software-key → adopt-node` for it in a world of its own.
- **driver** `script.py` — the latecomer learns node1's and node2's
  endpoints, node1 learns the latecomer's, node2 is told nothing. The
  latecomer calls `user.join` on node2, and the driver records the answer. After
  a 3 s pause, so an asynchronous push would have shown, it records the
  latecomer's `user.info` state and node1's and node2's `user.swarm_status`.
  Then the latecomer calls `user.join` on node1.
- **oracle** `verify.py` — the join through node2 was refused with the
  `sign as issuer` prefix (the contract's issuer is the User, and node2 holds
  no key for it; `mod/auth/src/signing.go` `SignIssuer`); after it the
  latecomer was `fresh` and node1's and node2's rosters held exactly node1
  and node2; the join through node1 was admitted with a contract issued by the
  User to the latecomer. On each node's own token, within 30 s, the
  latecomer is `claimed` by the same User, and `user.swarm_status` lists
  exactly node1, node2 and the latecomer, with `user.list_siblings` and the
  linked members exactly the other two.

`user.join` is the latecomer's request: the swarm node's join policy accepts
(the harness's `accept_all`), then it signs as the issuer, asks the latecomer's
`user.accept_membership`, indexes the contract and pushes it to its siblings.
The refusal comes at the signature, before the node is asked, indexed or
pushed, so the oracle can hold "no debris" to a fresh latecomer and unchanged
rosters. The match is on the prefix, not the sentence after it, which names
the key and the ways to hold one.

astral-docs `user-and-swarm.md` says any node of the swarm brings in new
nodes; only a node that holds the User's key can sign as the issuer, and this
test pins what astrald does.
