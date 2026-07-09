# Transport

* Upper layers are transport-agnostic; a transport registers under `exonet` by network name.
* Link strategies live in [links](links.md).

## Layer Stack

```
Session          — one routed Query, flow-controlled
  └─ Mux         — multiplexes sessions; encodes/decodes nodes/frames
       └─ Link   — channel.Channel with locked writes over a noise.Conn
            └─ brontide / noise.Conn — Noise XK; secp256k1 auth; ChaCha20-Poly1305
                 └─ exonet.Conn — raw transport bytes (tcp / kcp / tor / gw)
```

* `Link` embeds `*channel.Channel`; the channel's binary sender/receiver encodes `frames.Frame` objects on the wire.
* `Mux` shares the link's channel and serialises sends via `channel.WithLockedWrites()`; that lock is the concurrency contract that keeps one frame send atomic across the shared writer.

## Exonet Registry

* `exonet` is the pluggable transport registry.
* A transport registers three interfaces by network name: `exonet.Dialer`, `exonet.Parser`, and `exonet.Unpacker`.
* `exonet.Conn` is a raw unauthenticated byte stream with endpoint metadata.
* The `Endpoint` interface (network name + address, a serializable Object) is defined in astral-go `api/exonet` — see [mod.nodes.endpoint_with_ttl](../../system/protocols/nodes/types/mod.nodes.endpoint_with_ttl.md).
* `Endpoint.Pack()` must round-trip through the matching `Unpacker` for its `Network()`.
* Registry setters use `Replace`; a later registration overwrites the prior handler for that network.
* `ErrUnsupportedNetwork` and `ErrDisabledNetwork` are the reject sentinels for missing or disabled transport support.
