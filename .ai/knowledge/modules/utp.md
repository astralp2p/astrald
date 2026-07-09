# mod/utp

Adds a uTP (Micro Transport Protocol over UDP) transport to the node: listens for incoming uTP connections, dials `utp:host:port` endpoints, and registers the `utp` network with `exonet` for parsing, unpacking, and dialing. Also publishes the local listen endpoint per local IP as a `nodes.EndpointResolver` for the node's own identity.

## Dependencies

| Module | Why |
|---|---|
| `exonet` | `SetDialer`, `SetParser`, and `SetUnpacker` for the `utp` network; `Dial` returns `exonet.Conn` |
| `nodes` | `AddResolver(mod)` publishes local uTP endpoints; `Nodes.EstablishInboundLink` runs the brontide handshake on accepted connections |
| `ip` | `IP.LocalIPs` enumerates addresses used to build local `utp.Endpoint`s in `ResolveEndpoints` |
| `objects` | injected for completeness in `Deps`; not used directly by current flows |
| `core` | module registration and dependency injection |

## Flows

- Load config: `Loader.Load` reads YAML into `Config` -> strips an optional `utp:` prefix from each configured endpoint string -> `utp.ParseEndpoint` -> stores results in `mod.configEndpoints`.
- Wire transport: `LoadDependencies` injects deps -> `Exonet.SetDialer/SetParser/SetUnpacker("utp", mod)` -> `Nodes.AddResolver(mod)`.
- Run server: `Run` launches `tasks.Group(NewServer(mod))` -> `Server.Run` resolves `:ListenPort`, calls `utp.Listen` -> spawns an accept goroutine pushing `*utp.Conn` to `connCh` -> on accept, `WrapUtpConn(..., outbound=false)` -> `Nodes.EstablishInboundLink(ctx, conn)` in its own goroutine.
- Shutdown: `ctx.Done()` triggers `utpListener.Close()` from the watcher goroutine; the accept goroutine exits on the resulting listener error, and `Server.Run` returns nil when the context is cancelled.
- Dial: `Dial(ctx, endpoint)` rejects non-`utp` networks with `exonet.ErrUnsupportedNetwork` -> `utp.Dialer{Timeout: DialTimeout}.Dial` -> parses local and remote addresses back into `utp.Endpoint` -> returns `WrapUtpConn(..., outbound=true)`; closes the raw conn on any post-dial error.
- Resolve local endpoints: `ResolveEndpoints(ctx, nodeID)` returns an empty channel for non-self IDs; otherwise emits one `nodes.EndpointWithTTL{utp.Endpoint{IP, ListenPort}, 7d}` per local IP.
- Parse and unpack: `Parse("utp", addr)` calls `utp.ParseEndpoint`; `Unpack("utp", data)` decodes a `utp.Endpoint` via `Endpoint.ReadFrom`.

## Source

- `mod/utp/module.go` - `Module` interface (`exonet.Dialer`+`Unpacker`+`Parser`+`ListenPort`) and `ModuleName`. The `utp.Endpoint` wire object (text/JSON/binary forms) lives in astral-go `api/utp` (see astral-go .ai/knowledge/api/utp.md).
- `mod/utp/src/loader.go`, `mod/utp/src/module.go`, `mod/utp/src/deps.go`, `mod/utp/src/config.go` - registration, YAML config (`ListenPort` default 1791, `DialTimeout` default 1m), dependency injection, and exonet/nodes wiring.
- `mod/utp/src/server.go` - listener lifecycle, accept loop, and inbound link handoff.
- `mod/utp/src/dial.go`, `mod/utp/src/conn.go` - outbound dialing and the `WrappedConn` adapter that pairs raw `utp.Conn` with `exonet.Endpoint`s and an outbound flag.
- `mod/utp/src/parse.go`, `mod/utp/src/unpack.go` - network-tagged dispatch into `utp.ParseEndpoint` and the binary `Unpack` reader.
- `mod/utp/src/endpoint_resolver.go` - `nodes.EndpointResolver` for the local identity only.
- `mod/utp/views/endpoint_view.go` - terminal renderer using the astral-go `astral/log/theme` palette.

## Invariants

- All `exonet` dispatchers reject networks other than `utp` with `exonet.ErrUnsupportedNetwork`.
- `ResolveEndpoints` only ever returns endpoints for the node's own identity; remote identities resolve to an empty channel.
- Each published local endpoint has a 7-day TTL.
- `Server.Run` returns nil on context cancellation even if the accept loop reports an error.
