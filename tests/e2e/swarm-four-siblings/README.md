# swarm-four-siblings

The User adopts node3 and node4 into the swarm of node1 and node2. The swarm,
not the driver, links every pair, and every roster lists the same four
members. This is the setup test of the four-node swarm-comms flows (F1-F4,
M14).

- **after** `adopt-node` (node1 and node2 in the User's swarm) ·
  **destroys** `true` — node3 and node4 join the swarm, so every later test in
  the world sees four members.
- **driver** `script.py` — node1 learns node3's and node4's endpoints, node3
  and node4 learn node1's, node2 learns nothing; the User adopts node3, then
  node4, on node1. The driver polls each node's links and `user.swarm_status`
  every 0.5 s, up to 30 s after node4's adoption, and records the
  `four_siblings` fact: `{adopted_node3_at, adopted_at, mesh_at, links,
  rosters, roster_errors}`.
- **oracle** `verify.py` — every node lists a link to each of the other three
  within 30 s of node4's adoption, and again at the oracle, read live; every
  node's `user.swarm_status`, read live at the oracle, lists exactly the four
  identities, itself included. No source bounds roster sync, so the rosters
  the driver recorded are diagnosis only.

node2 was never told an endpoint of node3 or node4, and node3 and node4 were
never told each other's. A link node2 holds to a new node, or node3 to node4,
is the evidence that the adoption's contract push, the first-hand endpoint
pull and the forwarded notice carried each new node's endpoints to every
member (astral-docs `protocols/nodes/README.md`, the pushed-contract pull and
First-hand notice; each pull is bounded to 20 s).

The 30 s mesh bound is derived: one forwarded notice and one pull bounded to
20 s, with slack. `relay-contract-race` holds four nodes joined the same way
to the same 30 s (`driver/main.go`, `converge`).
