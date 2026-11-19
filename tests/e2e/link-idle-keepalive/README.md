# link-idle-keepalive

An idle healthy link stays up across many keepalive intervals, and each end
probes it about once per idle interval N.

- **after** `adopt-node` (node1 and node2 linked siblings) · **destroys**
  `true` — the world is left whole, but the planner requires it of a test in
  a world after w0.
- **config** tcp and kcp `link_idle_ping` and `link_ping_timeout` 2 s on both
  nodes, and `nodes.log_pings`, so each pong logs `ping with <peer>`.
  `link-dead-peer-keepalive` shares this world.
- **driver** `script.py` — taps both nodes' logs (`lib/nodelog.py`), waits
  until both ends' link ids hold still for 5 s, then watches 40 s: both
  ends' link ids every second, and each end's `ping with` and `closed` lines
  about the other. It records the `idle_keepalive` fact.
- **oracle** `verify.py` — a link both ends held at the start is held by
  both in every snapshot; no end closed a link to the other for
  `ping timeout`; each end received 0.5 to 1.5 pongs per link per N.

Spec: astral-docs `topics/link-mux.md`, Keepalive.
