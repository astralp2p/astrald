# swarm-apps-talk

Four apps started together on four siblings find each other through a
`reach=swarm` follow, and each first call to each reported provider answers,
over tcp and again over kcp alone. This is F1 of the E2E tests exhibit, the
flow of the report "first connection to a new provider fails with
`route_not_found`" (taD5d1d8Zn) with the relay-contract push on
(zuIhqtHhEG, DFOeFfSVqd). The push-off half is `relay-contract-race` (M1) and,
over kcp, `swarm-apps-matrix-kcp` (M14).

- **after** `swarm-four-siblings` (four siblings, every pair linked) ·
  **destroys** `true` — the kcp phase turns tcp dial and listen off on every
  node through the tree.
- **host_listener**: each mesh app binds its handler on the test host at its
  node's `host_ip`.
- **driver** `script.py`, two phases:
  - `tcp`: a mesh app (`tests/e2e/internal/meshapp`, `lib/meshapps.py`)
    starts on each node in one pass, service `f1-tcp`, `period=0`. Each app
    advertises, follows the service with `reach=swarm`, and calls each
    provider the follow reports once. The driver waits at most 60 s until
    every app finished a first call to each of the three remote providers,
    keeps the states and call logs, and stops the apps;
  - `kcp`: `lib/kcponly.py` turns tcp dial and listen off on every node,
    gives each node the others' kcp rows, closes every non-kcp link and links
    each pair over kcp; then the four apps start again with service `f1-kcp`,
    so with fresh identities and fresh contracts;
  - it records `swarm_apps_talk`: `{tcp, kcp_join, kcp}`, each phase
    `{starts, complete, states, calls, links_start, links_end,
    tcp_settings}`.
- **oracle** `verify.py`, per phase:
  - the four apps started within 2 s of each other;
  - 12 of 12 ordered pairs: the provider was reported and the first call
    answered `ok`;
  - each first call ended at most 10 s after the report;
  - each consumer called each provider once, and each provider served each
    remote consumer exactly once;
  - phase `kcp`: the join settled, every link at the phase's start and end is
    kcp, and tcp dial and listen read false on every node.

A call routes through the relay contract the consumer's node holds, by the
registration push or the first-link sync, or pulls on a miss (astral-docs
`topics/app-routing.md`, Resolving the host; Reach). The 10 s bound is the
umbrella's discovery-to-first-query bound (operator decision D11,
2026-11-20); it has margin against the 3 s pull wait. A mesh app records a
call `ok` only when the reply is `hello, <the calling app>`.
