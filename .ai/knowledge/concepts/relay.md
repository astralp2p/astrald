# Relay

## Wire

* Relayed queries travel as `frames.RelayQuery` over the link mux to the relay node.
* `RelayQuery` wraps a `Query` frame with `CallerID` and `TargetID`.
* The relayed session uses the same `Nonce` as a direct session and is opened on the link to the relay, not the target.

## Routing

* `Module.RouteQuery` selects relays only after direct and `RetrieveLink(StrategyBasic, StrategyTor)` fail within a 120-second timeout.
* `ExtraRelayVia` is the key into `q.Extra`; its value is `[]*astral.Identity`.
* Relays in `ExtraRelayVia` are tried in order; the target identity is skipped as its own relay.
* Before sending, when `ctx.Identity() != q.Caller`, the router pushes the caller proof from `ExtraCallerProof` to the relay via `objects.Push`.
* `Mux.RouteQuery` emits `RelayQuery` instead of `Query` whenever `q.Caller != ctx.Identity()`, regardless of connectivity to the target.

## Authorization

* `Mux.handleRelayQuery` checks `Auth.Authorize(RelayForAction)` whenever `RelayQuery.CallerID` differs from the link's remote identity.
* `AuthorizeRelayFor` grants when `Actor == ForID`.
* `ActionRelayFor = "mod.nodes.relay_for_action"` lives in `mod/nodes/module.go`.
* Rejected relay queries get a `Response` frame with `CodeRejected` and no session is launched.

## Inbound Session

* On accept, the relay's mux records `SourceIdentity = RemoteIdentity` of the link peer and `RemoteIdentity = CallerID`.
* The relayed inbound query is launched with `origin = OriginNetwork` so the local router treats it as remote.
