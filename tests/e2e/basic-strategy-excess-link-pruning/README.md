# basic-strategy-excess-link-pruning

A forced basic-strategy round to a node with several live endpoints and one
refused endpoint leaves exactly one new link, the same on both ends, and
closes every other link it made as excess.

- **after** `adopt-node` (node1 and node2 linked siblings) · **destroys**
  `true` — node1's book keeps the extra endpoints for 90 days, and no op
  removes one; node2 keeps three extra listeners; both keep one more link
  per round.
- **driver** `script.py` — as node2, opens TCP listeners on three free host
  ports, L2, L3 and L4 (`tcp.new_ephemeral_listener`, `lib/ports.py`). As
  node1, adds `tcp:<node2 host>:<port>` to its book for L2, L3, L4 and a
  fourth free port D that nothing listens on. Records both nodes' link ids to
  each other and node1's book. With a `log.listen` tap on node1, runs rounds
  of `nodes.new_link` as the User on node1 with `strategies=basic` and no
  endpoint, 2 s apart, recording each answer: at least 3, and more until
  node1 has logged an excess line, at most 20. Keeps node1's
  `closed out-link with <node2> (tcp): excess link` lines. Judges nothing.
- **oracle** `verify.py` — node1's book lists node2's own port P1, L2, L3,
  L4 and D. Once both nodes' link ids to each other stay put for 5 s: node1
  holds the baseline plus exactly the returned ids, node2 holds the
  same ids, and each new link is outbound tcp on node1 and inbound on node2
  (`lib/linkview.py`). Every round answered a link and the ids are new and
  distinct. node1's tap is whole and holds at least one excess-link
  line, and no more than one per losing tcp endpoint a round.

why no endpoint: `new_link` with one dials exactly it (`CreateLinkTask`,
astrald `mod/nodes/src/op_new_link.go`), which is what adopt-node does, and
the strategy never runs. Without one it schedules an `EnsureLinkTask` that
forces a new link, and `BasicLinkStrategy.Signal`
(`mod/nodes/src/basic_link_strategy.go`) dials every tcp endpoint of the
book in parallel, keeps the first link and closes later ones with
`ErrExcessLink`.

why the log: no op reports why a link closed, so node1's link pool line
(`mod/nodes/src/link_pool.go` `AddLink`) is the only witness of
`ErrExcessLink`. A loser still in its Noise handshake when the winner
cancels the round is closed without becoming a link, so a round may have
fewer excess lines than losers.

note: node1's book also holds the endpoints node2 publishes for itself, one
per advertisable interface address of its host, so the number of losers
depends on the host. node2 publishes no IPv6 link-local and no container or
host-only bridge address (`ip.Module.LocalIPs`, astrald
`mod/ip/src/advertisable.go`). The oracle's upper bound is read from the book.

Calibration: a loser is logged as excess only if it finishes negotiating and
joins the pool before the winner's cancel. A loser still negotiating then is
aborted (`errLateNegotiation`, `mod/nodes/src/link_negotiator.go`) and logs
no line. Since that change about 35% of 3-round runs saw no excess line, so
about 70% of single rounds log none. The driver therefore runs rounds until
one excess line shows, at most 20: a run without one is about 0.7^20, 8e-4.
The earlier figure of 6 rounds in 200 without a line, on a world before that
change, no longer holds.
