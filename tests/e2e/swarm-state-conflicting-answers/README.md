# swarm-state-conflicting-answers

Criterion 2.5: when two Privileged nodes answer the same request differently,
every node ends with the Decline.

- **after** `swarm-state-member` (node1 and node2 Privileged, node3 a Member).
- **driver** `script.py` — an app registers on node3, which files its
  ContractRequest. Once node1 and node2 list it pending, the User answers it
  at once on both: `approve` on node1 and `decline` on node2
  (`user.answer_request` routed through node1 to node2, so node2 signs the
  Decline). Records `conflicting_answers`.
- **oracle** `verify.py` — node1 signed the Approval and node2 the Decline;
  within 60 s, node1, node2 and node3 each store both answers and list the
  request declined.

**Strictness.** With an Approval taking precedence over a Decline
(`swarmSnapshot.requestStatus`), the oracle fails: "not every node ends with
the Decline within 60 s".
