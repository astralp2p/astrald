# route-outcomes-dead-peer

A peer that keeps its link open and never answers costs a caller the routing
deadline and reads as `timeout`; a link that goes down while a query waits on
it answers `unavailable` (rejection code 7) at once.

- **env** node · **start** `two-nodes` · **saves** — · **stops** node2
- **driver** `script.py` — runs `driver/` (Go, this repository's astral-go).
  node1 registers a caller app, and the driver waits until node2 holds its
  relay contract. The caller queries `node2:no.such.op`:
  - `live`: before anything happens, as a control (`route_not_found`);
  - `frozen`: after node2's astrald gets SIGSTOP. The kernel keeps the link's
    TCP connection up, so node1 sends the query and no answer comes back;
  - `killed`: sent while node2 is still frozen; 3 s later node2 gets SIGKILL,
    its kernel closes the connection, and node1's link goes down while the
    query waits.
- **oracle** `verify.py` — `frozen` reads `timeout` at about 60 s, the routing
  deadline, and is retryable; `killed` reads rejection code 7 within 1 s of
  the kill and is retryable.

A frozen process stands in for a black-holed link: astrald on node1 sees a link
that takes its bytes and never answers. Unlike a black hole, the peer's kernel
still acknowledges the bytes, so TCP never gives up on its own.

The test finds node2's process by the `-root` it runs with and signals it, so
it runs in local worlds only. node2 stays dead; the manifest stops it.

Spec: astral-docs `core-definitions/query.md`, Routing outcomes, and
`topics/link-mux.md`, sessions on a closed Link.
