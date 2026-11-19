# kcp-only-restart-relink

Two siblings that reach each other over kcp alone: after node2 restarts it
relinks to node1 over kcp within 5 s of its link task starting, node1 drops
the link it held to node2's old process, and a dial to a kcp address with
nothing behind it fails within seconds.

- **after** `adopt-node` (node1 and node2 linked siblings) · **restart**
  `node2` · **destroys** `true` — tcp dial and listen stay off in both
  nodes' trees.
- **driver** `script.py` — each node learns the other's kcp rows (the kcp
  endpoints the other resolves for itself, and its kcp port at its
  `lan_endpoint` address); tcp dial and listen go off through the tree on
  both nodes, so the setting survives the restart, and every non-kcp link
  between the pair closes; node1 links to node2 at node2's kcp row as the
  User; both ends settle on kcp links alone. Then node1 dials node2 at
  `kcp:<node2's address>:919`, where nothing listens, with a 90 s budget,
  timed on the host's clock. It records the `kcp_restart_relink` fact.
- **runner** — restarts node2 between driver and oracle.
- **oracle** `verify.py` — before the restart the pair held kcp links
  alone; the dead dial answered an error, no link, within 10 s; tcp dial and
  listen still read false on both nodes; within 60 s node2 holds a kcp link
  to node1, and node2's log shows it linked within 5 s of `starting to
  maintain link to <node1>`; node1 lists none of its old links to node2
  within 35 s of the oracle's start (kcp `link_idle_ping` 15 s plus
  `link_ping_timeout` 10 s, plus slack); a query from node1 to node2 as the
  User is answered, with no `nodes.close_link`.

why each criterion fails without its fix:

- relink: before astrald PR #497 the basic strategy dialed tcp rows alone,
  so node2's link task ended in `link not produced` and backed off.
- drop: before astrald PR #506 a kcp link had no idle probe, and node1 kept
  the link to node2's old process pooled as up.
- dead dial: a kcp dial is connectionless, and before this change
  `WrappedConn` cleared `dial_timeout` at the first queued write
  (`mod/kcp/src/conn.go`), so the Noise handshake waited for the caller's
  budget. `dial_timeout` now holds until the first frame the peer sends, and
  its default is 5 s (`mod/kcp/src/config.go`).

Distinct from `kcp-only-relink` (`kcp.suite`, VM), which drops tcp with an
op and restarts node2 while its driver watches; this test runs in a local or
Docker world.
