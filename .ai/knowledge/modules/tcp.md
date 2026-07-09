# mod/tcp

Adds TCP/IP connectivity for exonet links, including persistent inbound listening, outbound dialing, endpoint publication, and short-lived ephemeral listeners. Owns runtime listen and dial settings under the tree, TCP endpoint parsing and unpacking, and local endpoint advertisement for the node identity.

## Dependencies

| Module | Why |
|---|---|
| `exonet` | `LoadDependencies` registers the module as dialer, parser, and unpacker for `tcp`; `Dial` returns exonet network errors |
| `nodes` | `acceptAll` and server accept handlers call `EstablishInboundLink`; `LoadDependencies` registers the module as an endpoint resolver |
| `ip` | `endpoints` reads `LocalIPs`; `PublicIPCandidates` returns public TCP endpoint IPs |
| `tree` | `LoadDependencies` binds `/mod/tcp/settings` to runtime `listen` and `dial` values |
| `nearby` | `ComposeStatus` attaches TCP endpoints when nearby mode is visible |
| `objects` | injected into `Deps`; currently retained without direct calls |
| `core/assets` | `LoadYAML` reads TCP config and `assets.Database` is not used directly by this module |

## Flows

- Config load: `Load` reads YAML -> strips optional `tcp:` prefix from configured endpoints -> parses each endpoint -> stores valid static endpoints for later publication.
- Settings bind: `LoadDependencies` injects deps -> binds `Settings` at `/mod/tcp/settings` -> registers exonet dialer, parser, unpacker, and nodes resolver.
- Runtime settings sync: `Run` writes non-nil YAML `Dial` and `Listen` values into the bound tree values -> follows `settings.Listen` -> toggles `sig.Switch` to start or stop the persistent server.
- Persistent inbound link: listen toggle enables server -> `startServer` opens `:<listen_port>` -> accept TCP connection -> enable keepalive -> `tcp.WrapConn(rawConn, false)` -> `Nodes.EstablishInboundLink`.
- Outbound link: `Dial` accepts only `tcp` and `inet` endpoint networks -> checks `settings.Dial` -> `net.Dialer` with configured timeout and 5-second keepalive -> wraps connection with `outbound=true`.
- Ephemeral listener create: `CreateEphemeralListener` locks the listener map -> rejects an existing port -> starts `NewServer(port, handler)` in a goroutine -> deletes the map entry when the server exits.
- Ephemeral listener close: `CloseEphemeralListener` looks up the port -> returns `ErrEphemeralListenerNotExist` if absent -> closes and deletes the listener.
- Endpoint resolve: `ResolveEndpoints` serves only the local node identity -> suppresses output when `settings.Listen` is false -> returns local IP endpoints plus configured endpoints with a 7-day TTL.
- Nearby and public-IP publication: visible nearby status attaches all endpoints; `PublicIPCandidates` filters endpoint IPs to public addresses.

## Source

- `mod/tcp/module.go`, `conn.go`, `errors.go` - public module interface, connection wrapper, and listener errors.
- `mod/tcp/src/loader.go`, `module.go`, `deps.go`, `config.go` - construction, runtime settings, dependency registration, and YAML sync.
- `mod/tcp/src/server.go`, `dial.go`, `ephemeral_listener.go` - persistent accept loop, outbound dialing, and ephemeral listener lifecycle.
- `mod/tcp/src/endpoint_resolver.go`, `status_composer.go`, `ip_candidate_finder.go` - endpoint publication to nodes, nearby, and IP discovery.
- `mod/tcp/src/parse.go`, `unpack.go` - text and packed endpoint decoding.
- `mod/tcp/src/op_*.go` - query handlers for ephemeral listener management.
- `mod/tcp/views/endpoint_view.go` - endpoint renderer.

The endpoint wire type, op-name constants, and typed client wrappers now live in astral-go `api/tcp` (see astral-go .ai/knowledge/api/tcp.md).

## Surface

| What | Why it matters |
|---|---|
| `tcp.new_ephemeral_listener`, `tcp.close_ephemeral_listener` | query surface for caller-managed inbound TCP channels |
| `exonet.Dialer`, `exonet.Parser`, `exonet.Unpacker` for `tcp` | lets exonet open and decode TCP endpoints |
| `nodes.EndpointResolver` | publishes local TCP endpoints to peer discovery |
| `nearby.Composer` and `ip.PublicIPCandidates` | advertises visible TCP endpoints and contributes public IP candidates |
| `/mod/tcp/settings` | runtime tree-backed listen and dial switches |

## Invariants

- `Dial` rejects networks other than `tcp` and `inet` with `exonet.ErrUnsupportedNetwork`.
- `settings.Dial=false` disables outbound dialing; `settings.Listen=false` disables inbound listening and endpoint resolution.
- YAML `listen` and `dial` are pointer values; nil leaves the existing tree-backed setting unchanged.
- Ephemeral listener ports are unique in the process map.
- Accepted TCP connections are wrapped with `outbound=false`; dialed connections are wrapped with `outbound=true`.
- `ResolveEndpoints` only emits endpoints for the local node identity.
