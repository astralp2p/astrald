# mod/tor

Adapts Tor hidden services into the exonet transport layer so the node can dial and accept links over onion endpoints. Owns the SOCKS5 dialer, control-port hidden-service lifecycle, and the persisted v3 onion key.

## Dependencies

| Module | Why |
|---|---|
| `exonet` | registers the `tor` dialer, parser, and unpacker |
| `nodes` | accept path calls `EstablishInboundLink`; registers the endpoint resolver |
| `tree` | binds `/mod/tor/settings` to runtime `listen` and `dial` values |
| `nearby` | `ComposeStatus` attaches the active onion endpoint in visible nearby mode |
| `core/assets` | `LoadYAML` reads config; `tor.key` is read and written through module assets |
| Tor control port | authenticates and issues `ADD_ONION`; listener close issues `DEL_ONION` |

## Invariants

* The persisted `tor.key` yields the same onion identity across restarts.
* Only `ED25519-V3:` private keys from the control port are accepted for persistence.
* The SOCKS5 proxy is built at module load; changing `tor_proxy` requires a restart.
* `Conn.LocalEndpoint()` returns an empty endpoint and inbound `RemoteEndpoint()` returns nil, because Tor hides the peer; anything reading endpoints off a Tor link gets nothing.
* Resolved endpoints carry a 90-day TTL.
* BUG: the YAML `listen=false` sync path in `loadSettings` writes `config.Dial` into `settings.Listen`; verify before relying on config-only listener disablement.
