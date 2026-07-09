# Transport

* Transport modules register under `exonet`.
* Supported transports: TCP, KCP/UDP, Tor, and gateway relay.
* Upper layers are transport-agnostic.

## Exonet

* `exonet` is the pluggable transport registry.
* `Endpoint` (network name + address, a serializable Object) is defined in
  astral-go `api/exonet` — see [mod.nodes.endpoint_with_ttl](../../system/protocols/nodes/types/mod.nodes.endpoint_with_ttl.md).
* `exonet.Conn` is a raw unauthenticated byte stream with endpoint metadata.
* Transport modules register a Dialer, Parser, and Unpacker.

## Layer Stack

```
Session          — one routed Query, flow-controlled
  └─ Mux         — multiplexes sessions; encodes/decodes nodes/frames
       └─ Link   — channel.Channel with locked writes over a noise.Conn
            └─ brontide / noise.Conn — Noise XK; secp256k1 auth; ChaCha20-Poly1305
                 └─ exonet.Conn — raw transport bytes (tcp / kcp / tor / gw)
```

* `Link` embeds `*channel.Channel`; the channel's binary sender/receiver
  encodes `frames.Frame` objects on the wire.
* `Mux` shares the link's channel and serialises sends via
  `channel.WithLockedWrites()`.

## Transports

| Transport | Protocol | Use |
|---|---|---|
| `tcp` | TCP/IP | direct, stable IPs |
| `kcp` | KCP/UDP | NAT traversal after hole-punch |
| `tor` | Tor hidden services | anonymity |
| `gw` | gateway relay | unreachable nodes |

## Link Strategies

* Race strategies in parallel.
* The first successful brontide handshake wins.

| Strategy | Approach |
|---|---|
| Basic | Resolve published endpoints, dial directly |
| NAT | Coordinate UDP hole-punch via `nat`, then dial KCP |
| Tor | Dial Tor hidden service endpoint |
