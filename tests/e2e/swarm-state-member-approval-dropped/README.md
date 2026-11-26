# swarm-state-member-approval-dropped

Criterion 2.6: an Approval signed by a Member is dropped on merge, never
stored or forwarded on any node.

- **after** `swarm-state-member` (node3 a Member).
- **driver** `script.py` — two apps register on node3. Once node1 and node2
  list both requests, the driver signs an Approval of the first with node3's
  own key and one of the second with node2's own key (each node's
  `data/node_key`, read with `lib/nodefs`), and offers each with
  `user.swarm_push` to the signer's node and the other two. It waits 20 s,
  past one SwarmState pull (15 s). Records `member_approval`.
- **oracle** `verify.py` — node2's Approval (the control, same route) is
  stored on all three and its request reads approved; node3's Approval is
  stored on none, and its request is still pending on all three.

`user.answer_request` refuses an answer outside the node's tier and writes
nothing, and `crypto.sign_hash` refuses the node key, so an offered entry is
how a Member's Approval meets a merge.

**Strictness.** With every valid tier allowed to answer
(`swarmEntryCheck.answerGate`), the oracle fails: "node3's (a Member's)
Approval is stored on ['node1', 'node2', 'node3']".
