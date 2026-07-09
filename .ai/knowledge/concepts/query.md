# Query

A `Query` requests a bidirectional `Session` with a named service on a target
`Identity`. It is the base communication operation.

The `Query`/`InFlightQuery` wire types, the `Origin*` values, and the
`RouteQuery`/`ErrRouteNotFound` routing contract are provided by astral-go —
see astral-go .ai/knowledge/concepts/query.md.

## Routing

* `core/router.go`'s `Router` wraps `routing.PriorityRouter` from astral-go
  `lib/routing` and tracks live connections.
* The node runs registered routers in priority order.
* Outcomes: accept opens the session, reject stops routing, not found falls
  through to the next router.

## Preprocessors

* Preprocessors run before routing, in registration order.
* They may attach metadata, add `relay_via` hints, or block the query.
* `core` discovers them via `injectLoaded`. apphost uses one to attach
  AppContracts.

## Gateway

Gateway relays for nodes unreachable directly (NAT, firewall). The
application sees a normal session.
