# mod/nodes

Establishes and maintains authenticated encrypted links to peer nodes, then multiplexes query sessions across those links for network-zone routing. Owns endpoint persistence, link strategy activation, Noise and mux negotiation, flow-controlled session I/O, relayed query authorization, and session migration between links.

## Dependencies

| Module | Why |
|---|---|
| `exonet` | `Dial`/`Parse`/`Unpack` transport endpoints for basic, tor, and NAT-backed links |
| `crypto` | supplies the local secp256k1 private key for the Noise XK handshake |
| `dir` | resolves identities in ops; registers the `linked` identity filter via `SetFilter` |
| `auth` | registers the `RelayForAction` authorizer; `Authorize` gates inbound `RelayQuery` |
| `objects` | `objects.Push` delivers caller proofs for relayed routing; registers describer, finder, and receiver |
| `scheduler` | schedules create-link, ensure-link, and endpoint-cleanup tasks |
| `events` | emits link lifecycle, link pressure, and observed endpoint events |
| `user` | `LocalSwarm` refreshes endpoints after the first link |
| `nat`, `kcp`, `services` (opt) | NAT strategy discovers the NAT service, punches a UDP hole, then dials through KCP |
| `core/assets` | YAML config, endpoint database, loader setup, and migration |

## Invariants

* `Mux.SetRouter` is one-shot via `routerOnce` CAS; inbound queries block in `waitRouter` until `LinkPool.AddLink` sets the router.
* Inbound `AddLink` notifies watchers with a nil strategy; outbound strategies notify by name and shed excess concurrent links with `ErrExcessLink`.
* Session state CAS: `stateRouting` -> `stateOpen` on accepted `Response`; `stateRouting` -> `stateClosed` on rejection; `stateOpen` -> `stateMigrating` only in `migrator.Begin`; `Close` stores `stateClosed` unconditionally.
* `InputBuffer.Push` is all-or-nothing; overflow returns `ErrBufferOverflow`; writes after `Close` return `ErrBufferClosed`.
* `Data` payloads are chunked at `maxPayloadSize` (8 KB) by the per-session write callback.
* Session credit is per-session: `muxSessionWriter` blocks on the `OutputBuffer` wake channel when credit is exhausted; the peer's `onRead` sends a `Read` frame granting credit as it drains its `InputBuffer`.
* Auto-migration requires `minSessionAge` (30 s) or `minSessionBytes` (1 MB); manual migration via `OpMigrateSession` requires `IsOpen()`.
* `RelayForAction` grants only when `Actor == ForID` and constraints are nil or empty.
* `LinkPressureDetector` is attached only by `TorLinkStrategy`; other links report `PressureHigh() == false`.
* Per-target `NodeLinker` is kept in `LinkPool.linkers` keyed by identity string; the `linkWatcher` channel has capacity 1, so excess notifications are dropped.
* The connectivity upgrade holds a per-peer `sig.Switch` (one upgrade at a time), forces a new link with `WithForceNew()` + `StrategyNAT`, and re-enters only after a 5-minute cooldown.
* Noise handshake wraps the `exonet.Conn` in `channel.New(..., WithLockedWrites())`; the link and its mux share that one channel, and `mux2` is the negotiated version.
* `nodes__endpoints` rows are keyed by (identity, network, address) with optional expiry; the cleanup task removes expired rows after `CleanupGrace`.

## Flows

Ordering and state-machine sequences; pure call-path narration lives in the ops and spec.

* Session migration: initiator opens `nodes.migrate_session` -> `migrator.Begin` pauses the writer and swaps reader/writer buffers -> `Ready`/`Switched` `MigrateSignal` exchange -> each side sends a `frames.Migrate` on the old link -> `WaitClosed` resolves when the peer's `Advance` drains and closes the old `InputBuffer` -> `Resume`/`Done` -> `Complete` moves the session between mux maps and unblocks the writer, all within `migrateSessionTimeout`.
* Connectivity upgrade: `ReceiveObject` sees `LinkPressureEvent` -> per-peer `sig.Switch` avoids overlapping upgrades -> prefer a sibling link with no pressure detector or low pressure -> otherwise `RetrieveLink` with `WithForceNew()` and `StrategyNAT` under a 3-minute cap -> `migrateSessions` for eligible sessions -> 5-minute cooldown.
* Strategy activation: `RetrieveLink` returns an existing link unless `WithForceNew()` -> per-target `NodeLinker` signals each requested strategy and waits for all `Done()` -> the first matching `linkWatcher` receives the link.
* Endpoint reflection and cleanup: inbound links push `ObservedEndpointMessage` to the peer -> peer extracts public TCP or UTP IPs -> a bounded observed-endpoint cache emits `NewObservedEndpointEvent`; the cleanup task removes expired endpoint rows after `CleanupGrace`.
