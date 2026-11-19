# expel-sweep-redelivers

A member that lost a ban while it keeps a stable link to the issuer gets the
ban back from the issuer's expulsion sweep within one sweep period.

- **after** `adopt-node` (node1 and node2 in the User's swarm) ·
  **destroys** `true`: a ban cannot be lifted · **local_only**: the driver
  deletes a row from node2's database file.
- **config** node1's `user.reconcile_interval` is `5s` (default 1 h), so one
  sweep falls inside the test.
- **driver** `script.py`: the User expels a fresh identity on node1 and node2
  receives the ban by the expel push. The driver records node2's links to
  node1, deletes the ban row from node2's database while node2 runs, and polls
  node2's `user.list_expelled` until the ban is back.
- **oracle** `verify.py`: one row was deleted; node2 lists the ban, issued by
  the User, again within 30 s; and node2 holds no link to node1 that the
  driver did not record before the deletion, so the ban came over the
  existing link from the sweep (`mod/user/src/reconcile_task.go`), not from a
  sync at a new link.

Deleting the row stands in for a push node2 missed: no link event follows it,
which is the case the sweep exists for.
