# mod/fwd

Bridges byte streams between configured endpoints by accepting traffic on a local server and routing it to an astral, TCP, or Tor target. Owns static forwarding setup, per-forward server runners, and the protocol adapters that translate accepted streams into routed queries or outbound sockets.

## Dependencies

| Module | Why |
|---|---|
| `astral.Node` | supplies local identity; routes astral target queries through `node.RouteQuery` |
| `dir` | resolves identity names in `astral://caller@target:query` targets |
| `tor` (opt) | required for `tor://` targets; parses and dials Tor sockets |
| `core/assets` | `LoadYAML` reads the `fwd` config and its `Forwards` map |

## Invariants

* `AstralServer.Run` is intentionally non-functional and returns `obsolete`; configured astral inbound forwards fail after startup.
* `tor://` targets require `mod.Tor`; missing Tor makes target parsing fail.
* `mod.ctx` includes `ZoneNetwork` for all configured forwards.
* TCP and Tor dial failures call `query.Reject()` and return that rejection error.
* Server and target URIs must contain `://`; unsupported protocols fail before a runner starts.
* TCP inbound spawns one goroutine per accepted client and enforces no connection cap.
