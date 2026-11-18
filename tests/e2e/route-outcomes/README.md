# route-outcomes

An app on node1 queries targets that fail for different causes, and each query
ends with the answer of its own cause, fast, and with the retry hint the
cause calls for.

- **env** node · **start** `two-nodes` · **saves** —
- **driver** `script.py` — runs `driver/` (Go, this repository's astral-go).
  node1 registers a caller app, and the driver waits until node2 holds its relay
  contract (`apphost.hosts` on node2 names node1), so node2 admits relays for
  it. The caller then queries, recording the outcome, the time it took and
  `apphost.IsRetryable`:
  - `unknown_identity`: a freshly generated identity no node knows;
  - `no_such_op`: `node2:no.such.op` over the live link;
  - `refused`: `node2:shell.shell`, which node2 refuses for the app;
  - `filtered`: an unknown identity under the `linked` directory filter;
  - `handler_gone`: a provider app on node2 advertises
    `route-outcomes-echo` on its own session and serves `echo`; once a swarm
    discovery from node1 lists it and a call is accepted (`provider_served`),
    the provider's handler ends while its advertisement stays, and the
    caller queries it again. Discovery still lists the provider afterwards.
- **oracle** `verify.py` — `unknown_identity`, `no_such_op` and `handler_gone`
  read `route_not_found`, `refused` reads rejection code 1, and `filtered`
  reads `target_not_allowed`, each in under 100 ms and not retryable.

`no_such_op` and `handler_gone` cross the link: node2 finds no route and
answers the link's "no route here" value (`mod/nodes/src/mux.go`, `linkCode`),
which node1 reads as a missing route. A refusal crosses as code 1. `filtered`
never leaves node1: `core.Router` returns the directory filter's
`ErrTargetNotAllowed` as it is (`core/router.go`).

Spec: astral-docs `core-definitions/query.md`, Routing outcomes.
