# swarm-state-member-approval-dropped

Criterion 2.6: an offer signed by a Member is dropped on merge, never
stored or forwarded on any node. A contract request's yes is an offer since
the two-signature model.

- **after** `swarm-state-member` (node3 a Member).
- **driver** `script.py` — two apps register on node3. Once node1 and node2
  list both requests, the driver signs an offer for the first with node3's
  own key and one for the second with node2's own key (each node's
  `data/node_key`, read with `lib/nodefs`), and offers each with
  `user.swarm_push` to the signer's node and the other two. It waits 20 s,
  past one SwarmState pull (15 s). Records `member_approval`.
- **oracle** `verify.py` — node2's offer (the control, same route) is
  stored on all three and its request reads offered, or fulfilled once node3
  co-signed it; node3's offer is stored on none, and its request is still
  pending on all three.

`user.answer_request` refuses an answer outside the node's tier and writes
nothing, and `crypto.sign_hash` refuses the node key, so an offered entry is
how a Member's offer meets a merge.

**Strictness.** With every valid tier allowed to answer
(`swarmset.offerGate`), the oracle fails: "node3's (a Member's)
offer is stored on ['node1', 'node2', 'node3']".
