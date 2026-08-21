# mod/ether

Disseminates signed objects to peers on the local network over UDP and delivers verified inbound broadcasts into the local object receiver. Owns the UDP socket, link-local broadcast address selection, and the inbound event object that surfaces packet source IPs.

## Dependencies

| Module | Why |
|---|---|
| `crypto` | signs outbound hashes with `NodeSigner().SignHash`; verifies inbound with `VerifyHashSignature` |
| `objects` | receives the inbound event as the local node, then the inner object as `Broadcast.Source` |
| `core/assets` | `LoadYAML` reads `udp_port` |

## Invariants

* Signatures always use the local `NodeSigner`; a non-local `source` arg yields packets peers reject, because verify uses `Source`.
* Self-originating packets are filtered before the signature check via `Source.IsEqual(node.Identity())`.
* `LANDiscoveryHook` is not safe for concurrent assignment with `Run`; set it before the node starts.
* Socket bind failure at Load is non-fatal; a nil socket makes `Run` a no-op and `Push`/`PushToIP` return `socket not initialized`.
* Broadcast targets are deduped by IP; `169.254.0.0/16` and `fe80::/10` are skipped.
* Inbound delivery order: the receive event fires as the local node identity, then the inner object as `Broadcast.Source`.
* Max datagram is `65535` (`maxBroadcastSize`); usable payload is smaller after framing and signature.
