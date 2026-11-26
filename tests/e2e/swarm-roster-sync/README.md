# swarm-roster-sync

A third node joins a two-node swarm, and the swarm, not the driver, tells the
second node about it.

- **after** `adopt-node` (node1 and node2 in the User's swarm) ·
  **destroys** `true` — the swarm others rely on now holds node3.
- **driver** `script.py` — node1 and node3 learn each other's endpoint,
  node2 learns nothing; node3 joins through node1 as a Member, and the User
  approves its JoinRequest.
- **oracle** `verify.py` — within 30 s: node2's `user.swarm_status` lists
  node3 as linked and node2's `nodes.links` holds a link to node3; node3's
  `user.info` names the User; node3's `user.swarm_status` lists node1 and
  node2 as linked.

Since node2 was never told node3's endpoint, a link from node2 to node3 is
the evidence that the SwarmState sync carried node3's membership to a sibling
and the endpoint pull that follows it carried node3's endpoint (astral-docs
`user-and-swarm.md`, Swarm; astrald `mod/nodes/src/endpoints_changed.go`,
`LearnMemberEndpoints`). Live, node2 linked node3 within 0.5–1 s.
