# route-outcomes-expelled-host

A provider app on node2 is called from an app on node1; once the User expels
node2 from the swarm, the next call reads as a refusal (rejection code 1),
which `apphost.IsRetryable` calls final.

- **env** node · **start** `two-nodes` · **destroys** the world (the ban is permanent)
- **driver** `script.py` — runs `driver/` (Go, this repository's astral-go):
  node2 registers and serves a provider with `echo`; node1 registers a caller;
  the driver waits until each node holds the other app's relay contract, calls
  `echo` (`before`), expels node2 as the User (`user.expel` on node1), waits
  2 s, and calls again (`after`).
- **oracle** `verify.py` — `before` was accepted; `after` reads rejection code
  1 and is not retryable.

Spec: astral-docs `core-definitions/query.md`, Routing outcomes.
