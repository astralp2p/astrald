# Presence

Presence is how a node advertises its reachability to peers on the local network.

## Composition

* `mod/nearby` builds a `StatusMessage` by calling each registered `Composer`.
* `mod/nearby` knows nothing about TCP, KCP, or Tor; each transport appends its own endpoint objects.
* Adding a transport means adding a composer; discovery stays unchanged.

## Identity

* Visible mode resolves the sender from a `PublicProfile` (`NodeID`) or a signed contract (`Subject`).
* Stealth mode carries a `StealthHint`: a `sha256` commitment over the user ID and a nonce, plus the node ID XOR-masked by the user ID.
* Only peers that know the user identity can verify the commitment and unmask the node ID.
* Spec `protocols/nearby` pins the exact hint wire format and mask formula.

## Stealth

* Stealth transports attach nothing; only the `StealthHint` is present.
* Peers without the user identity see no endpoints and cannot recover the node ID.
* No hint means no broadcast.

## Size Limit

* Each attachment has a 4 KB cap from the UDP datagram constraint.
* Attach one address, not a routing table.
