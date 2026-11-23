# swarm-survives-churn

The last test of the F3 churn chain (flow F3 of the E2E tests exhibit): node3
freezes and thaws under the running `f3` mesh apps. Calls to the frozen
node's app end within the keepalive bound instead of hanging, the other apps
answer throughout, every pair answers within 60 s of the thaw, and the
chain's recovery times are tabulated.

- **after** `swarm-churn-leave` · **local_only** — it freezes node3's astrald
  by its pid (`lib/nodeproc.py`) · **destroys** `true` ·
  **host_listener** — the mesh apps serve on the test host.
- **driver** `script.py` — freezes node3 (SIGSTOP), holds 40 s (past tcp's
  N + T, 25 s), thaws it in `finally`, and waits up to 60 s until all 20
  pairs of the five apps answer since the thaw (`survives_churn`).
- **oracle** `verify.py` —
  - every call to node3's app in flight during the first 15 s of the freeze
    ended `unavailable` within 27 s of the freeze (or `ok` within 1 s, an
    answer sent before the stop), and each caller made at least one: a link
    closes no earlier than N + T after node3's last frame, so these calls
    wait on the link when it closes;
  - no call to node3's app lasted over 62 s;
  - the 12 pairs without node3 answered every call during the freeze;
  - every pair answered within 60 s of the thaw, and each pair's last call
    answered `ok`;
  - the recovery table of the chain, the join and the freeze held to 60 s;
  - it stops every `f3` app.

| exhibit step | test |
|--------------|------|
| 1 a node restarts | `swarm-churn-restart` |
| 2 a new node joins with its own app | `swarm-churn-join` |
| 6 an app registers while a sibling is down | `swarm-churn-late-app` |
| 3 a node leaves | `swarm-churn-leave` |
| 5 a peer freezes, then thaws | `swarm-survives-churn` |
| 4 a peer moves to a new address | none locally: `endpoint-change-relink` (VM) |

The freeze is last because it is `local_only`: the steps before it also plan
into a Docker world. A local node has no address to move, so the move step
is `endpoint-change-relink` in `swarm-comms-netsim.suite`.

Spec: `.ai/system/topics/link-mux.md` (Keepalive),
`.ai/system/core-definitions/query.md` (Routing outcomes). Bound: D11, 60 s
per churn step, tcp 27 s. Tickets: https://wiki.satforge.dev/doc/agqhgxZZrQ,
https://wiki.satforge.dev/doc/soYSZDACSo,
https://wiki.satforge.dev/doc/cyD4a6ojvQ

## Spec conflict

The exhibit says calls to a frozen peer "fail promptly rather than hang". The
spec bounds a call in flight at N + T, but a call started after the links
closed may wait for the 60 s routing deadline and end `timeout`. The oracle
holds every call in flight during the first 15 s of the freeze, before any
link can close, to 27 s, and every call to 62 s.

## Recovery markers

A step whose event the harness performs after the driver (the restarts)
records only the driver's end, so its table row includes the restart
itself; that step's own oracle holds its recovery to 60 s from the node's
return.
