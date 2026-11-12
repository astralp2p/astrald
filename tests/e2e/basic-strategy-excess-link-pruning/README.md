# basic-strategy-excess-link-pruning

A forced basic-strategy round to a node with several live endpoints and one
refused endpoint leaves exactly one new link, the same on both ends, and
closes every other link it made as excess.

- **after** `adopt-node` (node1 and node2 linked siblings) · **destroys**
  `true` — node1's book keeps the extra endpoints for 90 days, and no op
  removes one; node2 keeps three extra listeners; both keep three more links.
- **driver** `script.py` — as node2, opens TCP listeners on three free host
  ports, L2, L3 and L4 (`tcp.new_ephemeral_listener`, `lib/ports.py`). As
  node1, adds `tcp:<node2 host>:<port>` to its book for L2, L3, L4 and a
  fourth free port D that nothing listens on. Records both nodes' link ids to
  each other and node1's book. With a `log.listen` tap on node1, runs three
  rounds of `nodes.new_link` as the User on node1 with `strategies=basic`
  and no endpoint, 2 s apart, recording each answer. Keeps node1's
  `closed out-link with <node2> (tcp): excess link` lines. Judges nothing.
- **oracle** `verify.py` — node1's book lists node2's own port P1, L2, L3,
  L4 and D. Once both nodes' link ids to each other stay put for 5 s: node1
  holds the baseline plus exactly the three returned ids, node2 holds the
  same ids, and each new link is outbound tcp on node1 and inbound on node2
  (`lib/linkview.py`). Every round answered a link and the three ids are new
  and distinct. node1's tap is whole and holds at least one excess-link
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
per interface of its host (live: 127.0.0.1, a LAN address and a link-local
IPv6 address, 7 tcp endpoints in all), so the number of losers depends on
the host. The oracle's upper bound is read from the book.

Calibration, on a kept world of this test: 200 rounds of the driver's round,
each winner closed after it, on the 7-endpoint book above: 6 rounds with no
excess line, and 16, 35, 58 and 85 rounds with 1, 2, 3 and 4. Three rounds
all without one: about 3e-5 a run.
