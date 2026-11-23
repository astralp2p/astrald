# link-prefer-live

With two links to one peer and one of them dark, queries keep answering over
the live link, the dark link closes within N + T, and no query waits for the
routing deadline. This is M6 of the E2E tests exhibit
(https://wiki.satforge.dev/doc/e2e-tests-X3LHjItA2b).

- **after** `adopt-node` · **destroys** `true` · **host_listener** — node2
  links to node1 through TCP relays on the test host.
- **config** tcp and kcp `link_idle_ping = 4s`, `link_ping_timeout = 10s` on
  both nodes: a link turns suspect 4 s after its last frame and closes at
  14 s, a 10 s window in which only the live link may carry a new query.
- **driver** `script.py` with the Go program `driver/` (its relay is a copy
  of `reconnect-sync-after-dead-link`'s) —
  - phase A: node2 links to node1 through relay R1 and every other link
    closes; once R1 is quiet 1 s, node2 links directly (D, the newest); R1
    black-holes (drops every byte, keeps both connections open);
  - phase B: node2 links through relay R2 (the newest); once R2 is quiet
    1 s, it black-holes;
  - in each phase node1 queries node2's `user.info` every 0.5 s, each in its
    own goroutine with a 65 s deadline, for at least 15 s and until the dark
    link closed plus 5 s, while the driver polls both nodes' links every
    0.25 s (`prefer_live`).
- **oracle** `verify.py` —
  - each dark link closed on node1 within 16 s (N + T + 2) of its last
    forwarded byte;
  - phase A: every query answered `ok` within 2 s, at least 5 while node1
    still listed the dark link;
  - phase B: every query ended `ok` or `unavailable`, none `timeout`, each
    `ok` within 2 s or ended within 16 s of the black-hole; every query
    started from black-hole + N + 1 s until the close answered `ok` within
    2 s, at least 3;
  - after each close every query answered `ok`.

Spec: `.ai/system/topics/link-mux.md` (Keepalive, Detection bound),
`.ai/system/core-definitions/query.md` (Routing outcomes). Tickets:
https://wiki.satforge.dev/doc/soYSZDACSo,
https://wiki.satforge.dev/doc/0BumCfgzJE,
https://wiki.satforge.dev/doc/cyD4a6ojvQ,
https://wiki.satforge.dev/doc/agqhgxZZrQ

## Spec conflict

The exhibit says a query succeeds over the live link before the dead one
closes. The spec states no rule for choosing among duplicate links. The
oracle holds the spec's close bound hard and the before-close claim to
astrald's rule: a new query takes the newest link that is not suspect
(`mod/nodes/src/link_pool.go`, `SelectLinkWith`). Explicit `nodes.new_link`
links are not settled as strategy duplicates, so both links stay.
