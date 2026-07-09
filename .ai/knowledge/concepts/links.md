# Links, Sessions, and Link Establishment

## Link

* A `Link` is one authenticated, brontide-encrypted (Noise XK) connection
  between two `Identity` values — see [core-definitions/link](../../system/core-definitions/link.md).
* `Link` embeds `*channel.Channel`; reads and writes go through that channel
  with `WithLockedWrites()` so the mux can share the writer.
* `Link` implements `astral.Router` by delegating `RouteQuery` to its `Mux`.
* `Link` exposes `LocalIdentity()`, `RemoteIdentity()`, `Close()`,
  `CloseWithError()`, `Done()`, `Outbound()`, and `Network()`.
* A `Link` runs a continuous ping loop, tracks cumulative throughput, and may
  hold a `LinkPressureDetector`.
* `LinkCreatedEvent` and `LinkClosedEvent` report link lifecycle changes.
* Multiple links to the same peer may coexist; `SelectLinkWith` prefers a
  non-high-pressure one.

## Session

* A `Session` is a logical pipe over a `Link` for one routed `Query` — see [mod.nodes.session_info](../../system/protocols/nodes/types/mod.nodes.session_info.md).
* Each `Session` is tracked by a 64-bit `Nonce` and lives in `Mux.sessions`.
* `RemoteIdentity` is the logical peer; `SourceIdentity` is the link peer when
  the session is relayed, otherwise nil.
* Read and write halves are independent `muxSessionReader` and
  `muxSessionWriter` over per-direction buffers.
* Default receive buffer (`defaultBufferSize`) is 4 MB.
* `Data` payloads are chunked at `maxPayloadSize` (8 KB).
* `OutputBuffer.Write` returns `ErrBufferEmpty` when `wsize == 0`; the writer
  blocks on its wake channel.
* `Read` frames restore credit as the receiver drains its `InputBuffer`.
* States: `stateRouting`, `stateOpen`, `stateMigrating`, `stateClosed`;
  transitions go through `swapState` CAS.

## Link Lookup

* `LinkPool` returns an existing `Link` or registers a `linkWatcher`.
* `notifyLinkWatchers` delivers the new link to all pending waiters
  simultaneously.
* `RetrieveLink` accepts `WithStrategies(names...)` to limit which strategies
  are tried.
* `RetrieveLink` accepts `WithForceNew()` to bypass the existing-link check.
* `RetrieveLink` accepts `WithNetworks(names...)` to filter cached links by
  transport network.

## Strategy Activation

* `NodeLinker` holds strategy instances for one target.
* `Activate` calls `Signal(ctx)` on each named strategy.
* `Activate` returns a channel that closes when every signalled strategy is
  `Done`.
* `LinkStrategy` drives one link-establishment attempt.
* `LinkStrategy` exposes `Name()`, `Signal(ctx)`, and `Done()`.
* Built-in strategies:
  * `BasicLinkStrategy`: parallel direct dial across known endpoints.
  * `NATLinkStrategy`: UDP hole-punch to KCP.
  * `TorLinkStrategy`.
* `StrategyFactory` constructs a `LinkStrategy` for a target.
* `RegisterLinkStrategy` registers strategy factories per network.

## Link Pressure

* Link pressure starts when a `Link` score reaches a transport-specific
  threshold.
* The score is `WLevel*(level/LevelRef) + WRTT*(rttEma/RTTRef)`, where
  `level` is a leaky token-bucket of byte counts.
* `LinkPressureDetector` tracks the high state with hysteresis: `Enter`
  flips it on, `Exit` flips it off.
* Only `TorLinkStrategy` currently attaches a detector
  (`TorLinkPressureConfig`); other links report `PressureHigh() == false`.
* `DefaultLinkPressureConfig` exists in source but is not wired by any
  strategy.
* `LinkPressureEvent` reports pressure state changes.

## Connectivity Upgrade

On `LinkPressureEvent` (from `ReceiveObject`):

* A per-peer `sig.Switch` allows one upgrade at a time.
* Prefer any existing sibling link with no pressure detector or low pressure.
* If none exists, call `RetrieveLink` with `WithForceNew()` and `StrategyNAT`
  under a 3-minute timeout.
* On success, call `migrateSessions`; eligible sessions are open and either
  >= 30 s old or have transferred >= 1 MB.
* Each migration runs under `migrateSessionTimeout` (30 s).
* The switch holds for `upgradeCooldown` (5 minutes) before re-entry.
