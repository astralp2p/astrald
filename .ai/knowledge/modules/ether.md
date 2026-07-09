# mod/ether

Disseminates signed objects to peers on the local network over UDP and delivers verified inbound broadcasts into the local object receiver. Owns the UDP socket, broadcast packet format, link-local broadcast address selection, and inbound event object used to surface packet source IPs.

## Dependencies

| Module | Why |
|---|---|
| `crypto` | signs outbound broadcast hashes with `NodeSigner().SignHash`; verifies inbound signatures with `VerifyHashSignature` |
| `objects` | receives `EventBroadcastReceived` as the local node and then receives the inner object as `Broadcast.Source` |
| `core/assets` | `LoadYAML` reads `udp_port` from `ether.yaml` |

## Flows

- Socket setup: loader reads config -> `setupSocket` binds UDP on `:<udp_port>`; bind failure logs `LAN discovery disabled` and leaves the socket nil.
- Run: nil socket -> block until context end (no-op) -> otherwise call `LANDiscoveryHook(true)` when set -> start `broadcastReceiver` -> on context end close the socket and call `LANDiscoveryHook(false)`.
- Outbound broadcast: `Push` -> `makePacket` wraps object with timestamp and source -> hash signed broadcast payload with local node signer -> encode packet -> `broadcast`.
- Broadcast targets: `broadcast` reads `NetInterfaces()` -> keep up, broadcast-capable, non-loopback interfaces -> compute broadcast address for each CIDR -> skip duplicate and link-local addresses -> write one UDP datagram per target.
- Outbound unicast: `PushToIP` -> `makePacket` -> `writeToIP` sends one datagram to the requested IP on `udp_port`.
- Inbound read: `broadcastReceiver` loops on `readBroadcast` -> decode `SignedBroadcast` from UDP datagram -> ignore self-originating packets and unsigned packets -> verify signature with the public key derived from `Broadcast.Source` via astral-go `api/secp256k1.FromIdentity`.
- Inbound delivery: verified packet -> build `EventBroadcastReceived` with source IP and inner object -> `Objects.Receive(event, localID)` -> `Objects.Receive(inner object, Broadcast.Source)` -> log object ID when the inner receive succeeds.

## Source

- `mod/ether/module.go`, `broadcast.go`, `signed_broadcast.go`, `event_broadcast_received.go` - public interface and broadcast wire objects.
- `mod/ether/hooks.go` - `LANDiscoveryHook`, the global platform-resource hook invoked by `Run`.
- `mod/ether/src/loader.go`, `deps.go`, `config.go` - module registration, UDP port config, dependency injection, and socket setup call.
- `mod/ether/src/module.go` - run loop, packet creation, outbound broadcast/unicast, inbound verification, and delivery.
- `mod/ether/src/net.go` - swappable network-interface provider used by broadcast address selection.

## Surface

| What | Why it matters |
|---|---|
| `Module.Push` and `Module.PushToIP` | outbound LAN broadcast and direct UDP send entry points |
| `Broadcast` and `SignedBroadcast` | on-wire packet format and signed hash boundary |
| `EventBroadcastReceived` | local object notification that preserves source IP for inbound broadcasts |
| `NetInterfaces` | package variable that can be replaced by tests to control interface discovery |
| `LANDiscoveryHook` | global hook called by `Run` with `true`/`false` around the broadcast receiver; platform wrappers gate OS resources such as Android's multicast lock |
| `udp_port` | config value used for both UDP listen and send target port |

## Invariants

- UDP only: no ACK, retry, ordering, multicast.
- Socket bind failure at Load is non-fatal; with a nil socket `Run` is a no-op and `Push`/`PushToIP` return `socket not initialized`.
- `LANDiscoveryHook` is not safe for concurrent assignment with `Run`; set it before the node starts.
- Self-originating packets filtered before signature check via `Source.IsEqual(node.Identity())`.
- Signatures always use local `NodeSigner`; non-local `source` arg yields packets peers reject (verify uses `Source`).
- Broadcast targets deduped by IP string; `169.254.0.0/16` and `fe80::/10` skipped.
- Max datagram `65535` (`maxBroadcastSize`); usable payload smaller after framing + signature.
- `Push` defaults `source` to `node.Identity()` when nil.
- `udp_port` default `8822`, loaded from `ether.yaml`.
