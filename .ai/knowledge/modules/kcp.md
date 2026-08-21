# mod/kcp

Implements the `kcp` exonet transport over UDP for node links. Owns ephemeral listeners and remote-endpoint-to-local-port mappings used for NAT hole punching; the consumer is `mod/nodes/src/nat_link_strategy.go`.

## Dependencies

| Module | Why |
|---|---|
| `exonet` | registers the `kcp` dialer, parser, and unpacker; uses `EphemeralListener` |
| `nodes` | establishes inbound links for accepted sessions; registers KCP endpoint resolution |
| `nearby` | `ComposeStatus` attaches KCP endpoints only in visible nearby mode |
| `ip` | `LocalIPs()` for local endpoint resolution |
| `tree` | binds `/mod/kcp/settings` for runtime listen and dial changes |
| `xtaci/kcp-go/v5` | `ListenWithOptions`, `AcceptKCP`, `NewConn`, `UDPSession` |

## Invariants

* The ephemeral-listener ops and endpoint-local-port mapping ops exist for NAT hole punching; `nat_link_strategy.go` binds a mapped local port before dialing so the punched UDP path is reused.
* `SetEndpointLocalPort` with `Replace=false` fails on collision; `Replace=true` overwrites.
* `WrappedConn` applies `DialTimeout` only until the first successful read or write, then clears the session deadline.
* `Dial` requires a concrete `*kcp.Endpoint`; a generic endpoint with network `kcp` is not enough.
* Default listen port is `1792`; resolved endpoints carry a seven-day TTL.
