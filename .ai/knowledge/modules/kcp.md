# mod/kcp

Implements the `kcp` exonet transport by dialing and accepting reliable KCP sessions over UDP for node links. Owns the persistent listener switch, endpoint parsing and packing, ephemeral listener ops, and remote-endpoint-to-local-port mappings used for NAT hole punching.

## Dependencies

| Module | Why |
|---|---|
| `exonet` | registers `kcp` as a dialer, parser, and unpacker; uses `EphemeralListener` and network error sentinels |
| `nodes` | establishes inbound links for accepted KCP sessions and registers KCP endpoint resolution |
| `nearby` | `ComposeStatus` attaches KCP endpoints only when nearby mode is visible |
| `ip` | supplies `LocalIPs()` for local endpoint resolution and parses endpoint IP text |
| `tree` | binds `/mod/kcp/settings` so listen and dial settings can be changed at runtime |
| `objects` (opt) | injected in `Deps`; current source does not call it |
| `core/assets` | `LoadYAML` reads KCP config, endpoints, listen port, and dial timeout |
| `xtaci/kcp-go/v5` | provides `ListenWithOptions`, `AcceptKCP`, `NewConn`, and `UDPSession` |

## Flows

- Load and register transport: loader reads config, parses configured endpoints, and installs op handlers -> `LoadDependencies` binds tree settings -> registers `Dialer`, `Parser`, and `Unpacker` for `kcp` -> adds the module as a node endpoint resolver.
- Listen setting loop: `Run` seeds tree settings from config -> follows `settings.Listen` -> `sig.Switch` starts or stops `startServer`; nil listen setting is treated as enabled.
- Persistent inbound: `startServer` creates a `Server` on `ListenPort` -> `kcpgo.ListenWithOptions` -> `AcceptKCP` loop -> wrap session with local and remote endpoints -> `Nodes.EstablishInboundLink`.
- Outbound dial: `Dial` requires network `kcp`, checks `settings.Dial`, asserts `*kcp.Endpoint`, binds UDP port from `ephemeralPortMappings` when present, calls `kcpgo.NewConn`, parses local endpoint, and returns `WrappedConn`.
- Ephemeral listener ops: `kcp.new_ephemeral_listener` creates a `Server` keyed by port in `ephemeralListeners`; `kcp.close_ephemeral_listener` closes and removes that listener.
- NAT local-port mapping: `kcp.set_endpoint_local_port` parses the remote endpoint and records the local UDP port; `Replace=false` fails on collision, while `Replace=true` overwrites; `kcp.remove_endpoint_local_port` deletes the mapping.
- Mapping list: `kcp.list_endpoint_local_mappings` streams `EndpointLocalMapping` objects from the current mapping clone and ends with `EOS`.
- Endpoint resolution and nearby status: `ResolveEndpoints(local identity)` emits one `kcp.Endpoint` per `IP.LocalIPs()` using `ListenPort` and a seven-day TTL; `ComposeStatus` attaches the same local endpoints only in visible nearby mode.
- Endpoint codec: `Parse` accepts only network `kcp` and delegates `host:port` parsing to astral-go `api/kcp.ParseEndpoint`; `Unpack` accepts only network `kcp` and reads a binary `kcp.Endpoint` via `Endpoint.ReadFrom`. The endpoint's text/JSON string forms live with the wire object in astral-go `api/kcp` (see astral-go .ai/knowledge/api/kcp.md).

## Source

- `mod/kcp/module.go`, `mod/kcp/errors.go` - the public `Module` interface (`exonet.Dialer`+`Unpacker`+`Parser`+`ListenPort`), `ModuleName`, and node-side sentinels. The `Endpoint`/`EndpointLocalMapping` wire objects, `Method*` op-name constants, and typed client live in astral-go `api/kcp` (see astral-go .ai/knowledge/api/kcp.md).
- `mod/kcp/src/config.go`, `mod/kcp/src/loader.go`, `mod/kcp/src/deps.go`, `mod/kcp/src/module.go` - YAML config, endpoint parsing, dependency registration, settings binding, and listen switch.
- `mod/kcp/src/server.go`, `mod/kcp/src/ephemeral_listener.go` - persistent and ephemeral KCP listener lifecycle.
- `mod/kcp/src/dial.go`, `mod/kcp/src/conn.go` - outbound UDP binding, KCP dial, local-port mapping, and wrapped connection deadlines.
- `mod/kcp/src/parse.go`, `mod/kcp/src/unpack.go` - exonet endpoint text and binary codecs.
- `mod/kcp/src/endpoint_resolver.go`, `mod/kcp/src/status_composer.go` - node endpoint resolution and nearby status composition.
- `mod/kcp/src/op_new_ephemeral_listener.go`, `mod/kcp/src/op_close_ephemeral_listener.go`, `mod/kcp/src/op_set_endpoint_local_port.go`, `mod/kcp/src/op_remove_endpoint_local_port.go`, `mod/kcp/src/op_list_endpoint_local_mappings.go` - query operation handlers.
- `mod/kcp/views/endpoint_view.go` - terminal rendering for KCP endpoints.
- `mod/nodes/src/nat_link_strategy.go` - main NAT consumer of ephemeral listeners and local-port mappings.

## Surface

| What | Why it matters |
|---|---|
| `kcp` exonet dialer, parser, and unpacker | makes UDP KCP endpoints usable by node link establishment |
| `kcp.new_ephemeral_listener`, `kcp.close_ephemeral_listener` | create and remove temporary UDP listeners used by NAT strategies |
| `kcp.set_endpoint_local_port`, `kcp.remove_endpoint_local_port`, `kcp.list_endpoint_local_mappings` | manage the local UDP port reused when dialing a known remote endpoint |
| `/mod/kcp/settings` | runtime listen and dial switches bound through `tree` |
| `kcp.Endpoint` | exonet endpoint object for IP and UDP port; the type is defined in astral-go `api/kcp` |

## Invariants

- `Dial` requires a concrete `*kcp.Endpoint`; a generic endpoint with network `kcp` is not enough.
- `Parse`, `Unpack`, and `Dial` reject non-`kcp` networks with `exonet.ErrUnsupportedNetwork`.
- `Dial` returns `exonet.ErrDisabledNetwork` when `settings.Dial` is explicitly false.
- `ResolveEndpoints` returns endpoints only for the local node identity.
- `SetEndpointLocalPort` honors `Replace=false` as a collision guard.
- `WrappedConn` applies `DialTimeout` only until the first successful read or write, then clears the session deadline.
- Config `listen` and `dial` seed tree settings only when non-nil; defaults are enabled, listen port `1792`, and dial timeout `1m`.
- Configured endpoints are parsed into `configEndpoints`; current resolver/status code does not use them for resolution.
