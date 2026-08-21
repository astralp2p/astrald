# Query

Node-side routing pipeline. The `Query`/`Session`/`Origin*` wire types and the
`RouteQuery`/`ErrRouteNotFound` routing contract live in astral-go; routing
outcomes are in the spec. This note holds the daemon-side pipeline contracts.

## Routing

* `core/router.go`'s `Router` wraps `routing.PriorityRouter` from astral-go
  `lib/routing` and tracks live connections.
* The node runs registered routers in priority order.

## Preprocessors

* Preprocessors run before routing, in registration order.
* They may attach metadata, add `relay_via` hints, or block the query.
* `core` discovers them via `injectLoaded`. apphost uses one to attach
  AppContracts.
