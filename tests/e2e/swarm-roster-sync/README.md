# swarm-roster-sync

The User adopts a third node into a two-node swarm, and the swarm, not the
driver, tells the second node about it.

- **after** `adopt-node` (node1 and node2 in the User's swarm) ·
  **destroys** `true` — the swarm others rely on now holds node3.
- **driver** `script.py` — node1 and node3 learn each other's endpoint,
  node2 learns nothing; the User adopts node3 on node1.
- **oracle** `verify.py` — within 30 s: node2's `user.swarm_status` lists
  node3 as linked and node2's `nodes.links` holds a link to node3; node3's
  `user.info` names the User; node3's `user.swarm_status` lists node1 and
  node2 as linked.

Since node2 was never told node3's endpoint, a link from node2 to node3 is
the evidence that the adoption's push and roster sync reached a sibling and
carried node3's endpoint with it (astral-docs `user-and-swarm.md`, Swarm;
astrald `mod/user/src/op_adopt.go`, `PushToLocalSwarm` and
`NewSyncNodesTask`). Live, node2 linked node3 within 0.5–1 s.
