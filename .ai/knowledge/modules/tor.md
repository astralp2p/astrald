# mod/tor

Adapts Tor hidden services into the exonet transport layer so the node can dial and accept links over onion endpoints. Owns the SOCKS5 dialer, Tor control-port hidden-service lifecycle, persisted v3 onion key, and endpoint advertisement for the local node.

## Dependencies

| Module | Why |
|---|---|
| `exonet` | `LoadDependencies` registers the module as dialer, parser, and unpacker for `tor`; `Dial` returns `ErrDisabledNetwork` when dialing is switched off |
| `nodes` | server accept path calls `EstablishInboundLink`; dependency loading registers the module as an endpoint resolver |
| `tree` | `LoadDependencies` binds `/mod/tor/settings` to runtime `listen` and `dial` values |
| `nearby` | `ComposeStatus` attaches the active onion endpoint when nearby mode is visible |
| `core/assets` | `LoadYAML` reads Tor config; `tor.key` is read and written through module assets |
| Tor control port | `Server.listen` authenticates and issues `ADD_ONION`; listener close issues `DEL_ONION` |

## Flows

- Config and proxy setup: `Load` reads YAML -> creates `Server` -> builds a SOCKS5 proxy from `tor_proxy` and the configured dial timeout -> stores it as a `proxy.ContextDialer`.
- Dependency registration: `LoadDependencies` injects deps -> binds `/mod/tor/settings` -> registers the `tor` dialer, parser, unpacker, and nodes endpoint resolver.
- Runtime settings sync: `Run` writes configured `Dial` into tree settings -> the explicit `Listen=false` branch currently writes the configured dial value into `settings.Listen` -> follows `settings.Listen` -> toggles the hidden-service server through `sig.Switch`.
- Outbound link: `Dial` checks `settings.Dial` -> unpacks the endpoint -> applies the configured timeout -> dials `endpoint.Address()` through SOCKS5 -> wraps the connection with the remote onion endpoint and `outbound=true`.
- Hidden service startup: `Server.Run` loads or generates `tor.key` -> opens local TCP listener on `127.0.0.1:0` -> authenticates to the Tor control port -> sends `ADD_ONION` for the configured external listen port -> parses and stores the onion endpoint.
- Inbound link: hidden-service TCP listener accepts -> wraps connection with no remote endpoint and `outbound=false` -> `Nodes.EstablishInboundLink`.
- Hidden service shutdown: context cancellation closes the TCP listener -> listener close calls `DEL_ONION` for the service ID.
- Key persistence: missing or unreadable `tor.key` triggers `ADD_ONION NEW:ED25519-V3` through a temporary service -> extracts the returned private key -> writes it as `tor.key`.
- Endpoint resolve and nearby status: only the local node identity receives the active onion endpoint with a 90-day TTL; visible nearby status attaches the endpoint when it is non-zero.

## Source

- `mod/tor/module.go` - public module interface.
- `mod/tor/src/loader.go`, `module.go`, `deps.go`, `config.go` - config, SOCKS5 proxy setup, tree settings, dependency registration, and lifecycle.
- `mod/tor/src/dial.go`, `server.go`, `conn.go`, `parse.go`, `unpack.go` - outbound dialing, hidden-service server, connection wrapper, and endpoint decoding.
- `mod/tor/src/private_key.go` - `tor.key` loading, generation, and persistence.
- `mod/tor/src/endpoint_resolver.go`, `status_composer.go` - node endpoint resolution and nearby advertisement.
- `mod/tor/tc/` - Tor control protocol client, authentication, protocol info, and onion management.
- `mod/tor/views/endpoint_view.go` - endpoint renderer.

The onion endpoint object and its digest now live in astral-go `api/tor` (see astral-go .ai/knowledge/api/tor.md).

## Surface

| What | Why it matters |
|---|---|
| exonet `tor` dialer, parser, and unpacker | lets exonet route links over onion endpoints |
| hidden-service server | accepts inbound node links through a Tor v3 onion service |
| `nodes.EndpointResolver` | publishes the local onion endpoint when listening is active |
| `nearby.Composer` | includes the onion endpoint in visible nearby status |
| `/mod/tor/settings` and `tor.key` | runtime listen and dial switches plus durable onion identity |

## Invariants

- `settings.Dial=false` disables outbound Tor dialing; `settings.Listen=false` suppresses endpoint resolution and server startup.
- The same persisted `tor.key` yields the same onion service across restarts.
- Only `ED25519-V3:` private keys from the control port are accepted for persistence.
- `Conn.LocalEndpoint()` returns an empty Tor endpoint; inbound `RemoteEndpoint()` returns nil because Tor does not expose the peer endpoint.
- The SOCKS5 proxy is built at module load; changing `tor_proxy` requires a restart.
- Endpoint resolution serves only the local node identity and only after the hidden-service endpoint is non-zero.
- The YAML `listen=false` sync path in `loadSettings` uses `config.Dial` for the stored listen value; verify this before relying on config-only listener disablement.
