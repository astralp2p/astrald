# membership-resync-volume

Measures the traffic of the interim membership re-sync in a 10-node swarm
with 5 bans: the SyncNodesTask runs and pushes per link event, and the ban
pushes of the expulsion sweep per period.

- **after** `adopt-node` · **destroys** `true`: it expels identities.
- **config** every node's `user.reconcile_interval` is `10s` (default 1 h).
- **driver** `script.py`: the User adopts node3..node10 on node1 and expels
  five fresh identities. node2 closes its links to node3 three times; the
  driver counts the log lines of all ten nodes per link event. Then a quiet
  window of three sweep periods is counted. Counted lines, verbosity 1:
  `syncing with` (one SyncNodesTask run, `mod/user/src/sync_nodes_task.go`),
  `received mod.user.signed_expulsion` and `received mod.auth.signed_contract`
  (one accepted push each, `mod/objects/src/op_push.go`).
- **oracle** `verify.py`: every node holds the five bans; every link event
  re-linked and ran at least one SyncNodesTask; the quiet window pushed at
  least 5 bans to each of 9 siblings. The other counts are reported in
  `verify.log` and the facts, not judged.
