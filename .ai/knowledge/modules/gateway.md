# mod/gateway

Relays node links through a public gateway so nodes behind NAT can remain reachable, and maintains client-side registrations with configured remote gateways. Owns the `gw` exonet network, gateway registration state, idle relay sockets, connector reservations, and endpoint advertisement for gateway-routed links.

## Dependencies

| Module | Why |
|---|---|
| `dir` | resolves identities in `gw:<gateway>:<target>` endpoints |
| `exonet` | registers the `gw` dialer, parser, and unpacker; dials raw relay sockets for gateway handoff |
| `nodes` | establishes inbound links after relay handoff and registers gateway endpoint resolution |
| `scheduler` | gates persistent gateway tasks on `Ready()` and schedules `MaintainGatewayConnectionsTask` |
| `services` | advertises the `gateway` service when gateway mode is enabled |
| `tcp` | creates gateway TCP ephemeral listeners for configured relay networks |
| `ip` | supplies public IP candidates for default gateway endpoints and wakes maintain tasks on address changes |
| `nearby` | gates whether gateway endpoints are attached to nearby status in visible mode |
| `core/assets` | `LoadYAML` reads gateway config, networks, visibility, and persistent gateway targets |

## Flows

- Gateway startup: `Run` includes `ZoneNetwork` -> if `gateway.enabled`, `startServers` opens configured TCP relay listeners -> after `Scheduler.Ready()`, configured gateways are scheduled for persistent registration.
- Register with local gateway: `gateway.node_register` -> `register` checks `canGateway` -> builds configured or public TCP socket endpoint -> reuses existing `registeredNode` by caller identity or creates one with a stable nonce -> returns `Socket`.
- Maintain remote registration: task calls `client.Register` with configured visibility -> on success starts a `ConnPool`; failures retry with exponential backoff and can be woken by `EventNetworkAddressChanged`.
- Idle connection pool: `ConnPool` dials the returned socket endpoint, writes the registration nonce, and keeps at least `minIdleConns` connections; gateway `handleInbound` matches the nonce to a registered node and starts `idleConn.eventLoop`.
- Connect through gateway: `Dial(gw endpoint)` -> `client.Connect` -> gateway `reserveConn` claims one idle connection for the target -> creates a connector nonce and 30s expiry -> caller dials socket and presents connector nonce -> `handleInbound` activates the reserved idle connection and pipes both sockets.
- Relay handoff: gateway-side `idleConn.activate` closes `handoffCh` -> idle event loop exchanges handoff frames, marks ready, clears deadlines, and the gateway starts `pipe`.
- Query fallback route: if direct relay socket dial fails, `Dial` uses `node_route`; gateway accepts locally when `Target` is self, otherwise forwards another `node_route` query toward the target and pipes both query streams.
- Link bring-up at registered node: client-side idle pool wraps accepted relay connections as `gatewayConn` and calls `Nodes.EstablishInboundLink`.
- Advertise gateway endpoints: `ResolveEndpoints` returns `gw` endpoints only for the local identity; `ComposeStatus` attaches the same endpoints only when nearby mode is visible.
- List and unregister: `gateway.node_list` streams public registrations and ends with `EOS`; `gateway.node_unregister` closes idle connections for the caller and returns `Ack`.
- Shutdown: gateway mode closes pending connectors and registered nodes; client mode unregisters from each persistent gateway with a 10s context.

## Source

- `mod/gateway/module.go`, `mod/gateway/errors.go`, `mod/gateway/maintain_binding_task.go` - public module API, errors, and task type. The endpoint/socket/visibility wire objects live in astral-go `api/gateway`.
- Typed clients for register, connect, list, and unregister ops (called by the maintain task and dial path) live in astral-go `api/gateway/client`.
- `mod/gateway/src/loader.go`, `mod/gateway/src/module.go`, `mod/gateway/src/deps.go`, `mod/gateway/src/config.go`, `mod/gateway/src/server.go` - config, dependency registration, gateway listeners, and runtime cleanup.
- `mod/gateway/src/op_node_register.go`, `mod/gateway/src/op_node_connect.go`, `mod/gateway/src/op_node_list.go`, `mod/gateway/src/op_node_route.go`, `mod/gateway/src/op_node_unregister.go` - query operation handlers.
- `mod/gateway/src/accept.go`, `mod/gateway/src/registered_node.go`, `mod/gateway/src/connector.go`, `mod/gateway/src/connect.go`, `mod/gateway/src/idle_conn.go`, `mod/gateway/src/pool.go`, `mod/gateway/src/frames.go` - registration, idle-connection, connector, and handoff state machine.
- `mod/gateway/src/dial.go`, `mod/gateway/src/conn.go`, `mod/gateway/src/pipe.go`, `mod/gateway/src/parser.go`, `mod/gateway/src/unpack.go` - `gw` exonet dial, endpoint codec, wrapped connections, and stream piping.
- `mod/gateway/src/endpoint_resolvers.go`, `mod/gateway/src/service_discoverer.go`, `mod/gateway/src/status_composer.go` - node endpoint resolution, service discovery, and nearby composition.
- `mod/gateway/src/maintain_gateway_connections_task.go` - persistent remote gateway registration loop and network-address event wakeup.
- `mod/gateway/views/endpoint_view.go` - terminal rendering for gateway endpoints.

## Surface

| What | Why it matters |
|---|---|
| `gateway.node_register`, `gateway.node_unregister` | register or remove a node that wants to be reachable through this gateway |
| `gateway.node_connect` | reserves an idle connection and returns the socket nonce used by a caller to connect to a target |
| `gateway.node_route` | query-stream fallback for gateway link establishment |
| `gateway.node_list` | streams public registered identities |
| `gw` exonet network | lets peers represent a route through one gateway identity to one target identity |
| `MaintainGatewayConnectionsTask` | keeps client-side gateway registrations and idle pools alive |
| TCP gateway listener | accepts nonce-bearing relay sockets for registered nodes and connectors |

## Invariants

- `registeredNodes` is keyed by `identity.String()`; re-registering preserves idle connections and only updates visibility.
- Gateway ops are denied when `gateway.enabled` is false because `canGateway` returns false.
- Idle connection claims are one-shot through `idleConn.handoffOnce`; a connector also nils its reserved connection when taken.
- Connector reservations expire after `connectTimeout` and close their reserved idle connection if unused.
- Handoff constants are fixed in source: 30s ping interval, 60s ping timeout, 10s write timeout, 1s handoff polling, and 24h pipe idle timeout.
- `ConnPool` retries with 1s to 30s exponential backoff and returns `ErrSocketUnreachable` after three consecutive dial failures.
- `Dial` rejects gateway endpoints whose gateway identity is local; parser rejects endpoints where gateway and target are the same.
- `ResolveEndpoints` returns only local-identity endpoints with a seven-month TTL.
- `OpNodeList` emits only `VisibilityPublic` registrations and terminates with `EOS`.
