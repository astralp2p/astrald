# mod/exonet

Registers raw network transports so modules can dial, parse, and unpack endpoints by network name without depending on a specific transport. Owns the raw `Conn` contract and the per-network dialer, parser, and unpacker maps. The `Endpoint` interface is defined in astral-go `api/exonet` (see astral-go .ai/knowledge/api/exonet.md).

## Dependencies

| Module | Why |
| --- | --- |
| `dir` | injected via `core.Inject`; the registry itself currently does not call it |

## Flows

- Register transport: a transport module receives `exonet.Module` in its dependencies -> calls `SetDialer`, `SetParser`, or `SetUnpacker` with a network name -> registry replaces the previous handler for that name.
- Dial endpoint: `Dial(ctx, endpoint)` reads `endpoint.Network()` -> looks up the matching dialer -> delegates to transport dialer or returns `ErrUnsupportedNetwork`.
- Parse address: `Parse(network, address)` looks up a parser by network -> delegates text address parsing or returns `ErrUnsupportedNetwork`.
- Unpack endpoint: `Unpack(network, data)` looks up an unpacker by network -> delegates packed endpoint decoding or returns `ErrUnsupportedNetwork`.
- Lifecycle: loader reads empty config and dependency injection runs; `Prepare` is a no-op and `Run` waits for context cancellation.

## Source

- `mod/exonet/module.go`, `conn.go`, `errors.go` - public registry interface, raw connection contract, ephemeral listener shapes, and sentinels.
- `mod/exonet/src/loader.go`, `module.go`, `deps.go`, `config.go`, `prepare.go` - module registration, dependency injection, registry maps, dispatch, setters, and lifecycle.
- `mod/tcp/src/deps.go`, `mod/kcp/src/deps.go`, `mod/utp/src/deps.go`, `mod/tor/src/deps.go`, `mod/gateway/src/deps.go` - examples of transport modules registering exonet handlers.

## Surface

| What | Why it matters |
| --- | --- |
| `Module.Dial`, `Parse`, and `Unpack` | unified raw-network operations used by callers that only know a network name or endpoint |
| `SetDialer`, `SetParser`, and `SetUnpacker` | extension points for transport modules |
| `Endpoint` | transport-specific address object that can be displayed and packed; defined in astral-go `api/exonet` |
| `Conn` | raw unauthenticated byte stream with endpoint metadata |
| `ErrUnsupportedNetwork` and `ErrDisabledNetwork` | shared sentinel errors for missing or disabled transport support |

## Invariants

- Unknown networks always surface `ErrUnsupportedNetwork`.
- `Conn` is raw bytes plus endpoint metadata; no encryption or authentication.
- `Endpoint.Pack()` must round-trip through the matching `Unpacker` for `Network()`.
- Registry never verifies that dialer, parser, and unpacker are all present for a network.
- Setters use `sig.Map.Replace`; later registration silently overwrites prior.
- No background work; `Prepare` no-op, `Run` blocks on `ctx.Done()`.
