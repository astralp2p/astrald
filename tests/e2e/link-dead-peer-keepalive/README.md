# link-dead-peer-keepalive

A link whose peer stops responding closes within the transport's idle
interval N plus its ping timeout T, and a stream over it ends with it.

- **after** `link-idle-keepalive` (node1 and node2 linked siblings; tcp and
  kcp `link_idle_ping` and `link_ping_timeout` 2 s on both) · **destroys**
  `true` · **local_only** — it signals node2's astrald by its pid
  (`lib/nodeproc.py`).
- **driver** `script.py` — taps node1's log, waits until node1's links to
  node2 hold still, and opens `log.listen` on node2 through node1 as the
  User (`user_token`). It freezes
  node2 with SIGSTOP and at once queries `node2:user.info`; it polls node1's
  links every 0.2 s until none leads to node2, then resumes node2 with
  SIGCONT and polls both ends until they are linked again. It records the
  `dead_peer_keepalive` fact.
- **oracle** `verify.py` — node1 lists no link to node2 within N + T + 2 s
  of the freeze; node1 logged each closed for `ping timeout`; the stream
  ended within N + T + 2 s; the query ended with an error; the pair relinked
  within 60 s of the resume.

SIGSTOP keeps node2's sockets open, and its kernel still acknowledges what
node1 sends, so TCP never fails on its own: only the keepalive probe finds
the link dead.

The query is not timed yet: until ticket k8mv9KqtUy (Routing outcomes)
lands, a query waiting on a link that closes waits out its own 60 s routing
deadline. The oracle prints when it ended.

Spec: astral-docs `topics/link-mux.md`, Keepalive and Sessions.
