# Links, Sessions, and Link Establishment

Owns the link pool, link pressure, and connectivity upgrade. Session flow-control lives in [mux](mux.md); the layer stack lives in [transport](transport.md).

## Link

* A `Link` is one authenticated, brontide-encrypted (Noise XK) connection between two `Identity` values — see [core-definitions/link](../../system/core-definitions/link.md).
* `Link` implements `astral.Router` by delegating `RouteQuery` to its `Mux`.
* Multiple links to the same peer may coexist; `SelectLinkWith` prefers a non-high-pressure one.
* A `Link` runs a continuous ping loop, tracks cumulative throughput, and may hold a `LinkPressureDetector`.

## Link Lookup

* `LinkPool` returns an existing `Link` or registers a `linkWatcher`.
* `notifyLinkWatchers` delivers the new link to all pending waiters simultaneously.
* `RetrieveLink` accepts `WithStrategies(names...)` to limit which strategies are tried.
* `RetrieveLink` accepts `WithForceNew()` to bypass the existing-link check.
* `RetrieveLink` accepts `WithNetworks(names...)` to filter cached links by transport network.

## Strategies

* `RegisterLinkStrategy` registers a `StrategyFactory` per network; a factory constructs one `LinkStrategy` per target.
* `BasicLinkStrategy`: parallel direct dial across known endpoints.
* `NATLinkStrategy`: UDP hole-punch to KCP.
* `TorLinkStrategy`: dial the Tor hidden-service endpoint; attaches the pressure detector.

## Link Pressure

* Link pressure starts when a `Link` score reaches a transport-specific threshold and clears with hysteresis (`Enter`/`Exit`).
* Only `TorLinkStrategy` attaches a detector (`TorLinkPressureConfig`); other links report `PressureHigh() == false`.
* `LinkPressureEvent` reports pressure state changes.

## Connectivity Upgrade

On `LinkPressureEvent` (from `ReceiveObject`):

* A per-peer `sig.Switch` allows one upgrade at a time.
* Prefer any existing sibling link with no pressure detector or low pressure.
* If none exists, call `RetrieveLink` with `WithForceNew()` and `StrategyNAT` under a 3-minute timeout.
* On success, call `migrateSessions`; a session is eligible when open and either >= 30 s old or >= 1 MB transferred.
* Each migration runs under `migrateSessionTimeout` (30 s).
* The switch holds for `upgradeCooldown` (5 minutes) before re-entry.
