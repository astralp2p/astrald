# policy-delegate

node1 delegates the app registration decision to an app, and keeps the
authorizer assignments it records across a restart.

- **env** node · **start** `one-node` · **saves** — · **restart** `node1`
- **driver** `script.py` — runs `driver/` (Go, this repository's astral-go) in
  `drive` mode. A control-plane app on node1 serves the decision op and an
  authorizer op, and `apphost.set_app_register_policy` points the node at it:
  - **hold** — the delegate holds a registration 70 s, past the router's 60 s
    en-route bound (`core/router.go`, `routingTimeout`); the registering client
    receives its token.
  - **disconnect** — a registering client that closes its query ends the
    delegate's question within 10 s; `apphost.list_tokens` counts no new token.
  - **timeout** — the setter refuses `timeout=11m`; with `timeout=5s` a delegate
    that never answers has the registration refused 5 s to 30 s after the
    client asked.
  - **authorizer** — a decision naming an authorizer for `AdminManageApps`
    records it with the new app as actor; the new app's `apphost.list_tokens`
    asks that authorizer about the new app.
  - The driver restores the accept-all policy, removes the decision's
    assignment, then sets an action-wide and an actor-specific `SeeObjects`
    authorizer and writes both as facts.
- **restart** — the runner restarts node1 between driver and oracle
  (`test.toml` `restart`); session.json counts it in `nodes.node1.restarts`.
- **oracle** `verify.py` — every check held, node1 restarted, and `driver
  verify` lists both authorizers, removes the actor-specific one (the
  action-wide one stays), then removes the action-wide one.

The test runs before `adopt-node` in `main.suite`, so the restart never drops
a link another test stands on.
