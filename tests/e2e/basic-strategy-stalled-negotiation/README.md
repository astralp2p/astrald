# basic-strategy-stalled-negotiation

A peer that completes the Noise handshake and then stalls in mux negotiation
does not delay a forced basic-strategy round that has a winner, does not
block the next round, and node1 lets go of the stalled connection when the
round ends.

- **after** `bootstrap-user-software-key` (node1 alone) · **destroys**
  `true` — the fake peer's three endpoints stay in node1's book for 90 days,
  and no op removes one · **host_listener** — the fake peer listens on the
  test host, at node1's `host_ip`, for node1 to dial.
- **fake peer** `driver/main.go` — an identity X with no astrald behind it,
  on three TCP listeners. Each completes the Noise_XK responder handshake
  (`brontide`). `stall` then sends nothing and reads until node1 closes.
  `live2` and `live3` run the responder side of the mux2 negotiation
  (feature list, status 0, link nonce) and hold the link, answering pings.
  A live listener starts its negotiation once a `stall` connection sits open
  after its handshake (at most 3 s), so each round's winner arrives while a
  sibling dial is stuck. It records every accepted connection, with its
  handshake, negotiation and close times, and writes them to its state file
  (`lib/apps.py`) from a writer of its own, so a slow disk never delays a
  connection; the write at SIGTERM is complete and marks itself final.
- **driver** `script.py` — builds and starts the fake peer. As node1, with
  node1's own token, adds `stall` and `live2` to node1's book as endpoints
  of X, runs `nodes.new_link(X, strategies=basic)` with a 15 s timeout, adds
  `live3` and runs a second round with a 10 s timeout. Records each round's
  answer and times and node1's links to X. Judges nothing.
- **oracle** `verify.py` — both rounds answered a link within 5 s, with
  different ids. Once node1's links to X stay put for 5 polls, node1 holds
  exactly the two returned ids. It then stops the fake peer and judges its
  final state: in each round a live listener negotiated while a `stall`
  connection sat open after its handshake; `live3` accepted a connection
  during round 2; node1 closed every `stall` connection no later than 5 s
  after its round answered.

why it matters: a basic round reports its winner only after every dialer
returns (astrald `mod/nodes/src/basic_link_strategy.go` `Signal`,
`wg.Wait`), and a round still running makes the next `Signal` a no-op. The
round's `cancel()` ends a stalled Noise handshake
(`mod/nodes/src/noise/handshake.go`), but `NegotiateOutbound`
(`mod/nodes/src/link_negotiator.go`) reads with no deadline. A peer that
stalls after the handshake held the winner back until the caller's timeout,
and every later round with it.

why a fake peer: no astrald stops between its handshake and its negotiation,
so only a peer that speaks the handshake and then goes silent reaches this
path. Its identity is a raw key, which `nodes.add_endpoint` and
`nodes.new_link` resolve without a directory entry.
