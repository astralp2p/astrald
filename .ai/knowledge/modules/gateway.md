# mod/gateway

Relays node links through a public gateway so nodes behind NAT stay reachable, and maintains client-side registrations with configured remote gateways. Owns the `gw` exonet network, gateway registration state, idle relay sockets, connector reservations, and endpoint advertisement for gateway-routed links.

## Dependencies

| Module | Why |
|---|---|
| `dir` | resolves identities in `gw:<gateway>:<target>` endpoints |
| `exonet` | registers the `gw` dialer, parser, and unpacker; dials raw relay sockets for handoff |
| `nodes` | `EstablishInboundLink` after relay handoff; registers gateway endpoint resolution |
| `scheduler` | gates persistent gateway tasks on `Ready()` and schedules `MaintainGatewayConnectionsTask` |
| `services` | advertises the `gateway` service when gateway mode is enabled |
| `tcp` | opens gateway TCP relay listeners for configured networks |
| `ip` | supplies public IP candidates for default endpoints and wakes the maintain task on address change |
| `nearby` | gates whether gateway endpoints attach to nearby status in visible mode |
| `core/assets` | reads gateway config, networks, visibility, and persistent gateway targets |

## Flows

* Startup ordering: `Run` includes `ZoneNetwork`; if `gateway.enabled`, `startServers` opens configured TCP relay listeners; only after `Scheduler.Ready()` are configured gateways scheduled for persistent registration.
* Connect through gateway: `client.Connect` -> gateway `reserveConn` claims one idle connection for the target and creates a connector nonce with 30s expiry -> caller dials the socket and presents the connector nonce -> `handleInbound` activates the reserved idle connection and pipes both sockets.
* Relay handoff: `idleConn.activate` closes `handoffCh` -> the idle event loop exchanges handoff frames, marks ready, clears deadlines -> gateway starts `pipe`.
* Query fallback route: if the direct relay socket dial fails, `Dial` uses `node_route`; the gateway accepts locally when `Target` is self, otherwise forwards another `node_route` toward the target and pipes both query streams.
* Remote registration: the maintain task calls `client.Register`; on success it starts a `ConnPool`; failures retry with backoff and can be woken by `EventNetworkAddressChanged`.

## Invariants

* `registeredNodes` is keyed by `identity.String()`; re-registering preserves idle connections and only updates visibility.
* Gateway ops are denied when `gateway.enabled` is false, because `canGateway` returns false.
* Idle-connection claims are one-shot through `idleConn.handoffOnce`; a connector also nils its reserved connection when taken.
* Connector reservations expire after `connectTimeout` and close their reserved idle connection if unused.
* Handoff constants are fixed in source: 30s ping interval, 60s ping timeout, 10s write timeout, 1s handoff poll, 24h pipe idle, 30s connect-nonce expiry.
* `ConnPool` retries with 1s-30s exponential backoff, returns `ErrSocketUnreachable` after three consecutive dial failures, and keeps at least `minIdleConns` connections.
* `Dial` rejects gateway endpoints whose gateway identity is local; the parser rejects endpoints where gateway and target are the same.
* `ResolveEndpoints` returns only local-identity `gw` endpoints, with a seven-month TTL.
* `node_list` emits only `VisibilityPublic` registrations and terminates with `EOS`.
