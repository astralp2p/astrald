# swarm-member-restart

One member of a three-node swarm restarts, and the swarm is whole again: the
restarted node still holds its membership, and every node lists exactly the
expected members and siblings.

- **after** `swarm-roster-sync` (node1, node2 and node3 in the User's swarm) ·
  **restart** `node3` · **local_only** — only a local world can restart a node
  · **destroys** — see below.
- **driver** `script.py` — changes nothing. It waits until all three nodes
  agree on the whole roster (each node's `user.swarm_status` lists the three
  nodes, and links the other two), then records the three nodes' identities,
  each node's active-contract id from `user.info`, and node3's restart count.
- **runner** — restarts node3 between driver and oracle.
- **oracle** `verify.py` — node3 was restarted exactly once. On each node's
  own token: `user.info` answers `claimed` under the same User with the same
  contract id as before; and within 60 s `user.swarm_status` lists exactly the
  three nodes, with exactly the other two linked, and `user.list_siblings`
  lists exactly the other two. The sets are compared exactly, so a short
  roster, an extra member, or a node that is a member but not linked fails.

A node that reads its stored contract wrongly (or not at all) at start comes
back unclaimed or with another contract; a roster that is not rebuilt comes
back short; a link that is not re-formed leaves `list_siblings` short.

## Place in the plan

`swarm-roster-sync` and `expel-propagation` both destroy their world, and
`expel-propagation` continues `swarm-roster-sync`'s. This test is a second
child of `swarm-roster-sync`, listed after `expel-propagation` so that one
keeps the world and this test forks: the planner replays `bootstrap-user-
software-key → adopt-node → swarm-roster-sync` as prereqs in a world of its
own. It declares `destroys`, as every restarting test does: a restart replaces
a node's process, and `expel-propagation` should not run on a node this test
bounced (the selftest also holds every world after w0 to destroyers). A
failure here leaves `expel-propagation` untouched.
