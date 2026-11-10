# app-routing-two-apps

Two apps on node1, each with its own identity, token and client, serve the
same op (`rta_who`). A visitor app calls each of them by identity, in an
interleaved order, and is answered by the app it addressed. Each app records
every query the node delivered: only the queries addressed to its own
identity, from the visitor. A call to an app that registered but never served,
and a call to an identity the node has never seen, are refused with
`route_not_found` and reach neither app.

- **env** node · **start** `null` · **saves** —
- **driver** `script.py` — runs `driver/` (Go, this repository's astral-go) in
  `drive` mode:
  - the node registers alpha and bravo (`apphost.register`); each serves
    `rta_who` through `apps.ServeWith` on a client of its own token and
    answers with its own name;
  - the node registers a visitor and an idle app; the idle app never serves;
  - the visitor calls `rta_who` eight times, alpha and bravo interleaved
    (a, b, a, b, a, b, b, a), and each answer names the app addressed;
  - each app's own record holds exactly its calls, each with the app's
    identity as target and the visitor as caller;
  - the visitor calls the idle app and a fresh random identity; both fail
    with `route_not_found`, and neither app's record grows.
- **oracle** `verify.py` — every step held, plus the same story with fresh
  identities (`driver verify`).

`Module.RouteQuery` (`mod/apphost/src/query_router.go`) walks the registered
handlers and routes a query to one whose identity equals the query target; a
query with no such handler ends in `query.RouteNotFound()`. The refusal is
therefore decided by the router and is not an `apphost.ErrorMsg` from an
endpoint: `IPCHandler.RouteQuery` (`ipc_handler.go`) maps an `ErrorMsg`, or any
unexpected reply, from a dialled endpoint to the same `route_not_found`.
Both end as `route_not_found` for the caller.

Registration is additive (`op_register_handler.go`): each app's handler is a
separate entry keyed by its identity, so two apps may serve the same op name
without sharing a handler.

The driver calls once both apps' registration hooks have run: the registrar
reports `Ready` as soon as it binds, before it attaches a handler, and a hook
runs only after the attach (astral-go `lib/apps/registrar.go`). No call is
retried.

The apps' handlers end with the driver, as `apphost.bind` removes them when
its session closes. The apps' identities, tokens and grants stay on node1.
