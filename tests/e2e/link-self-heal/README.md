# link-self-heal

A sibling pair whose links an operator closes relinks on its own, five times
over, through the user module's maintain-link task, and the pairs not cut
keep their links.

- **after** `swarm-roster-sync` (node1, node2 and node3 in one swarm, each
  linked to the other two) · **destroys** `true` — the world is left whole,
  but the planner requires it of a test in a world after w0; it costs one
  forked world.
- **driver** `script.py` — opens a session and a `log.listen` tap
  (`lib/nodelog.py`) on each node, with the node's own token. It waits until
  the swarm is whole and two link snapshots 3 s apart agree. Then five
  cycles: node1-node2 closed on node1, on node2, on node1; node1-node3 closed
  on node1; node2-node3 closed on node2. A cycle closes each link the closer
  lists to its peer once (`nodes.close_link`), polls only local ops (both
  ends' `nodes.links` and `user.swarm_status`) every 0.25 s until both ends
  hold a link to each other on an id that was not closed, then keeps both
  ends' maintain-task log lines about the peer and snapshots all three pairs'
  link ids from both ends. A cycle that does not heal ends the cycles. It
  records the `link_heal` fact and judges nothing.
- **oracle** `verify.py` — on fresh sessions, after the links stay put for
  5 polls: both ends of every pair hold the same 1 to 2 link ids, each with
  `outbound` flipped and the identities swapped; those ids equal the
  driver's final snapshot; every `swarm_status` lists the three nodes and
  links the other two. From the fact: the baseline was whole and quiet;
  every cycle healed within 30 s on an agreed id that was not closed; the
  pairs not cut kept their ids in every cycle; in every cycle node A or
  node B logged `link to <peer> is broken, trying to reconnect`,
  `established` or `restored after`; the log taps were whole and dropped
  nothing.

why only local ops while healing: a query aimed at the peer makes the router
dial on demand (astrald `mod/nodes/src/query_router.go` `RouteQuery`) and
would heal a pair whose maintain task is dead.

why the log lines: only `MaintainLinkTask.Run`
(`mod/user/src/maintain_link_task.go`) logs them, so one per cycle shows the
task woke on the `LinkClosedEvent`, not some other dialer. A task that
started linked logs `established` on its first relink and nothing before it.

Not asserted, only printed: link counts per cycle (a cross-dial can leave
two), the heal time per cycle, which end logged the wake. Live, every cut
healed in under 1 s.
