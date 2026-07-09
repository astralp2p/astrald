# Relay

## Definition

* A `Relay` is a third-party node that forwards queries between two peers that
  cannot connect directly.
* A relay operates at the query layer, not the transport layer.
* A relay is distinct from the `gw` transport, which works at the byte-stream
  layer.

## Wire

* Relayed queries travel as `frames.RelayQuery` over the link mux to the relay
  node.
* `RelayQuery` wraps a `Query` frame with `CallerID` and `TargetID`.
* The relayed session uses the same `Nonce` as a direct session and is opened
  on the link to the relay, not the target.

## Routing

* `Module.RouteQuery` selects relays only after direct and `RetrieveLink` with
  `StrategyBasic` and `StrategyTor` fail within a 120-second timeout.
* `ExtraRelayVia` is the key into `q.Extra`; its value is
  `[]*astral.Identity`.
* Relays in `ExtraRelayVia` are tried in order.
* The target identity is skipped as its own relay.
* Before sending, when `ctx.Identity() != q.Caller`, the router pushes the
  caller proof from `ExtraCallerProof` to the relay via `objects.Push`.
* `Mux.RouteQuery` emits `RelayQuery` instead of `Query` whenever
  `q.Caller != ctx.Identity()`, regardless of connectivity to the target.

## Authorization

* `RelayForAction` is the typed auth action; `Actor` is the link peer
  requesting to relay, `ForID` is the original `CallerID`. The type is defined
  in astral-go `api/nodes` (astrald keeps the `ActionRelayFor` action-name
  constant in `mod/nodes/module.go`); its fields and `Constrainable`/`ApplyConstraints`
  behavior are specified at
  [mod.nodes.relay_for_action](../../system/protocols/nodes/types/mod.nodes.relay_for_action.md).
* `ActionRelayFor = "mod.nodes.relay_for_action"`.
* `Mux.handleRelayQuery` checks `Auth.Authorize(RelayForAction)` whenever
  `RelayQuery.CallerID` differs from the link's remote identity.
* `AuthorizeRelayFor` grants when `Actor == ForID`.
* Rejected relay queries get a `Response` frame with `CodeRejected` and no
  session is launched.

## Inbound Session

* On accept, the relay's mux records `SourceIdentity = RemoteIdentity` of the
  link peer and `RemoteIdentity = CallerID`.
* The relayed inbound query is launched with `origin = OriginNetwork` so the
  local router treats it as remote.
