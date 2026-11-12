# basic-strategy-stalled-endpoint-released

An endpoint that accepts the TCP connection and then says nothing does not
delay a forced basic-strategy round that has a winner, and node1 lets go of
the stalled connection when the round ends.

- **after** `adopt-node` (node1 and node2 linked siblings) · **destroys**
  `true` — the bait endpoint stays in node1's book for 90 days, and no op
  removes one; both nodes keep two more links · **local_only** — the bait
  listens on the host's loopback for node1 to dial.
- **driver** `script.py` — serves a bait (`lib/bait.py`) on 127.0.0.1: it
  accepts, reads and discards, never writes, and records each connection's
  accept time, bytes received and close time. As node1, adds the bait to its
  book as an endpoint of node2, beside node2's own live endpoints. Records
  both nodes' link ids to each other, then runs two rounds of
  `nodes.new_link` as the User on node1 with `strategies=basic`, no endpoint
  and a 20 s timeout, 3 s apart. Keeps the bait up until node1 has closed
  every connection, or 10 s after the last round. Judges nothing.
- **oracle** `verify.py` — each round answered a link within 10 s; the bait
  received bytes on at least one connection (node1 began a Noise handshake
  with it); every bait connection was opened during a round and closed by
  node1 no later than 5 s after that round answered, and none was open at
  the end. Once both nodes' link ids to each other stay put for 5 s, node1
  holds the baseline plus exactly the two returned ids, node2 the same, and
  both ends agree on each (`lib/linkview.py`).

why it matters: a basic round answers only after all its dialers return
(astrald `mod/nodes/src/basic_link_strategy.go` `Signal`, `wg.Wait`), so a
dialer stuck on a silent endpoint holds back the winner. The winner's
`cancel()` releases it: `noise.HandshakeOutbound`
(`mod/nodes/src/noise/handshake.go`) closes the connection when its context
ends. brontide declares a handshake read timeout and never uses it
(`brontide/noise.go`), so nothing else bounds the stall.

why a host clock: the bait and the driver run in one process, so a
connection's close time and a round's answer time compare directly.

Live: each round answered in about 12 ms; the bait received the 50 bytes of
Noise act one and saw node1 close the connection within the same round.
