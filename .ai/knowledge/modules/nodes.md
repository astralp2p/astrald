# mod/nodes

Establishes and maintains authenticated encrypted links to peer nodes, then multiplexes query sessions across those links for network-zone routing. Owns endpoint persistence, link strategy activation, Noise and mux negotiation, flow-controlled session I/O, relayed query authorization, and session migration between links.

## Dependencies

| Module | Why |
|---|---|
| `exonet` | dials and parses transport endpoints for basic, tor, and NAT-backed links |
| `crypto` | provides the local secp256k1 private key used by the Noise XK handshake |
| `dir` | resolves identities in ops and registers the `linked` identity filter with `SetFilter` |
| `auth` | registers `RelayForAction` authorizer and authorizes inbound `RelayQuery` frames |
| `objects` | pushes caller proofs for relayed routing and registers describer, finder, and receiver integrations |
| `scheduler` | schedules create-link, ensure-link, and endpoint-cleanup tasks |
| `events` | emits link lifecycle, link pressure, and observed endpoint events |
| `user` | `LocalSwarm` is used when refreshing endpoints after the first link |
| `nat`, `kcp`, `services` (opt) | NAT link strategy discovers NAT service, punches a UDP hole, then dials through KCP |
| `core/assets` | YAML config, endpoint database, loader setup, and migration |

## Flows

- Outbound link: strategy dials an `exonet.Conn` -> `noise.HandshakeOutbound` -> wrap in `channel.New(..., WithLockedWrites())` -> outbound negotiation chooses `mux2` and exchanges status and link nonce -> `newLink(outbound)` -> `LinkPool.AddLink` -> set mux router -> reflect observed endpoint.
- Inbound link: accepted connection -> `noise.HandshakeInbound` -> channel with locked writes -> inbound negotiation offers `mux2`, status, and link nonce -> `newLink(inbound)` -> `LinkPool.AddLink` -> notify link watchers.
- Network route: reject non-network zone and self-target queries -> prefer existing non-high-pressure link -> otherwise `RetrieveLink` with basic and tor strategies for 120 seconds -> otherwise try `ExtraRelayVia` identities and push caller proof when caller differs from context identity.
- Session open: `Mux.RouteQuery` creates outbound session -> sends `Query`, or `RelayQuery` when `q.Caller != ctx.Identity()` -> waits for `Response` -> accepted response opens the session, grows writer credit, and copies response bytes back to the caller writer.
- Session accept: inbound `Query` or `RelayQuery` -> relayed queries with a foreign `CallerID` require `Auth.Authorize(RelayForAction)` -> `createSession` -> wait for router assignment -> launch query with `OriginNetwork` -> send accepted or rejected `Response`.
- Frame loop: `Link.readLoop` reads objects from its `channel.Channel` -> `Mux.Handle` dispatches frames -> query frames run in goroutines -> `Data` frames push into `InputBuffer` -> `Read` frames grow writer credit -> `Reset` closes peer state.
- Flow control: `muxSessionWriter.Write` consumes `OutputBuffer.wsize` and chunks at `maxPayloadSize` -> writer blocks on the buffer wake channel when credit is exhausted -> peer reader drains `InputBuffer` and `onRead` sends a `Read` frame granting credit.
- Ping and pressure: link ping loop wakes after jitter, active checks, or `Wake()` -> ping timeout closes the link -> pong RTT and `onBytes` feed the optional pressure detector (currently only attached on Tor links).
- Session migration: initiator opens `nodes.migrate_session` -> `migrator.Begin` pauses writer and swaps reader/writer buffers -> `Ready`/`Switched` `MigrateSignal` exchange -> each side sends a `frames.Migrate` on the old link -> `WaitClosed` resolves when peer's `Advance` drains and closes the old `InputBuffer` -> `Resume`/`Done` -> `Complete` moves the session between mux maps and unblocks the writer, all within `migrateSessionTimeout`.
- Connectivity upgrade: `ReceiveObject` sees `LinkPressureEvent` -> per-peer `sig.Switch` avoids overlapping upgrades -> prefer sibling link with no pressure detector or low pressure -> otherwise `RetrieveLink` with `WithForceNew()` and `StrategyNAT` under a 3-minute cap -> `migrateSessions` for eligible sessions -> 5-minute cooldown.
- Strategy activation: `RetrieveLink` returns an existing link unless `WithForceNew()` -> per-target `NodeLinker` signals each requested strategy and waits for all `Done()` -> the first matching `linkWatcher` receives the link; the watcher channel has capacity 1 so excess notifications are dropped.
- Endpoint reflection and cleanup: inbound links push `ObservedEndpointMessage` to the peer -> peer extracts public TCP or UTP IPs -> bounded observed-endpoint cache emits `NewObservedEndpointEvent`; cleanup task removes expired endpoint rows after `CleanupGrace`.

## Source

- `mod/nodes/module.go`, `mod/nodes/errors.go`, `mod/nodes/tasks.go` - public API, constants, and task interfaces. The `RelayForAction` auth action, `MigrateSignal`, and the info/event objects (`EndpointWithTTL`, `LinkInfo`, `SessionInfo`, `NodeInfo`, link lifecycle/pressure events) live in astral-go `api/nodes`.
- `mod/nodes/frames/frame.go`, `mod/nodes/frames/ping.go`, `mod/nodes/frames/query.go`, `mod/nodes/frames/relay_query.go`, `mod/nodes/frames/read.go`, `mod/nodes/frames/response.go`, `mod/nodes/frames/data.go`, `mod/nodes/frames/migrate.go`, `mod/nodes/frames/reset.go`, `mod/nodes/frames/errors.go` - mux frame registry and wire encoders.
- `mod/nodes/src/loader.go`, `mod/nodes/src/deps.go`, `mod/nodes/src/module.go`, `mod/nodes/src/config.go` - config, database setup, strategy registration, resolver registration, and dependency hooks.
- `mod/nodes/src/link.go`, `mod/nodes/src/link_negotiator.go`, `mod/nodes/src/link_pool.go`, `mod/nodes/src/node_linker.go`, `mod/nodes/src/noise/handshake.go`, `mod/nodes/src/noise/conn.go` - link lifecycle, Noise handshake, negotiation, pooling, and strategy activation.
- `mod/nodes/src/basic_link_strategy.go`, `mod/nodes/src/tor_link_strategy.go`, `mod/nodes/src/nat_link_strategy.go` - dial race, tor retry and pressure behavior, NAT punch and KCP handoff.
- `mod/nodes/src/mux.go`, `mod/nodes/src/mux_sessions.go`, `mod/nodes/src/session.go`, `mod/nodes/src/mux_session_reader.go`, `mod/nodes/src/mux_session_writer.go`, `mod/nodes/src/input_buffer.go`, `mod/nodes/src/output_buffer.go`, `mod/nodes/src/session_migrator.go`, `mod/nodes/src/err_buffer_empty.go` - mux sessions, buffers, credit, and migration plumbing.
- `mod/nodes/src/query_router.go`, `mod/nodes/src/migrate_session.go`, `mod/nodes/src/connectivity_upgrade.go`, `mod/nodes/src/pressure.go` - network routing, migration orchestration, pressure-triggered upgrades, and pressure scoring.
- `mod/nodes/src/object_receiver.go`, `mod/nodes/src/object_describer.go`, `mod/nodes/src/object_finder.go`, `mod/nodes/src/ip_candidate_finder.go`, `mod/nodes/src/endpoint_resolver.go` - objects integration and endpoint discovery helpers.
- `mod/nodes/src/db.go`, `mod/nodes/src/db_endpoint.go`, `mod/nodes/src/db_endpoint_resolver.go`, `mod/nodes/src/cleanup_endpoints_task.go`, `mod/nodes/src/authorizers.go`, `mod/nodes/src/create_link_task.go`, `mod/nodes/src/ensure_link_task.go` - endpoint persistence, tasks, and relay authorizer.
- `mod/nodes/src/op_*.go` - query handlers for links, sessions, endpoints, link creation, close, and migration.
- Typed clients used by the strategies and migration flows live in astral-go `api/nodes/client`.

## Surface

| What | Why it matters |
|---|---|
| `nodes.links`, `nodes.sessions`, `nodes.new_link`, `nodes.close_link`, `nodes.add_endpoint`, `nodes.resolve_endpoints`, `nodes.migrate_session` | external inspection and link/session control surface |
| `Module.RouteQuery` | network-zone router that chooses direct links, creates links, or routes through relays |
| `Mux.RouteQuery` | link-local router that opens a framed session for one in-flight query |
| `StrategyFactory`, `EndpointResolver` | extension points for link creation and endpoint discovery |
| `nodes__endpoints` | persistent endpoint table keyed by identity, network, and address with optional expiry |
| link and session events | objects/events integration used by discovery, pressure upgrades, and status reporting |

## Invariants

- `Mux.SetRouter` is one-shot via `routerOnce` CAS; inbound queries block in `waitRouter` until `LinkPool.AddLink` sets it.
- Inbound `AddLink` notifies watchers with a nil strategy; outbound strategies notify with their name and lose excess concurrent links with `ErrExcessLink`.
- Session state CAS rules: `stateRouting` -> `stateOpen` on accepted `Response`, `stateRouting` -> `stateClosed` on rejection, `stateOpen` -> `stateMigrating` only in `migrator.Begin`; `Close` stores `stateClosed` unconditionally.
- `InputBuffer.Push` is all-or-nothing; overflow returns `ErrBufferOverflow` and writes after `Close` return `ErrBufferClosed`.
- `Data` payloads are chunked at `maxPayloadSize` (8 KB) by the per-session write callback.
- Auto-migration requires `minSessionAge` (30 s) or `minSessionBytes` (1 MB); manual migration via `OpMigrateSession` requires the session to be `IsOpen()`.
- `RelayForAction` grants only when `Actor` equals `ForID`; constraints must be nil or empty.
- Per-target `NodeLinker` is kept in `LinkPool.linkers` keyed by identity string; `Activate` signals each requested strategy once per call.
- `LinkPressureDetector` is only attached by `TorLinkStrategy`; other links report `PressureHigh() == false`.
