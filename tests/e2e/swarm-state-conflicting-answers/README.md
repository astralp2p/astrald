# swarm-state-conflicting-answers

Criterion 2.5: when two Privileged nodes answer the same request differently,
every node ends with the same status.

- **after** `swarm-state-member` (node1 and node2 Privileged, node3 a Member).
- **driver** `script.py` — an app registers on node3, which files its
  ContractRequest. Once node1 and node2 list it pending, the User answers it
  at once on both: `approve` on node1, an offer, and `decline` on node2
  (`user.answer_request` routed through node1 to node2, so node2 signs the
  Decline). The offer is refused when the Decline reached node1 first.
  Records `conflicting_answers`.
- **oracle** `verify.py` — node2 signed the Decline and node1 the offer, if
  any; within 60 s, node1, node2 and node3 each store the Decline and list
  the request with one status: declined, or fulfilled when node3 co-signed
  the offer first. A Decline only closes its request (PR 4 decision Q1).

**Strictness.** With nodes disagreeing on a fulfilled request's status
(`swarmset.Snapshot.RequestStatus`), the oracle fails: "the nodes do not
agree on declined or fulfilled within 60 s".
