# go-public

Gives a node a **public address on the LAN** so it can be the public peer of a punch with a
NAT'd node (`enter-nat`).

On each `--vm` (default `node2`):

- adds the TEST-NET alias **`198.51.100.<lan-octet>`** to the LAN NIC, the subnet every NAT'd
  node's public alias lives on, so the node answers a NAT'd node from a public address;
- restarts astrald, because its `nat` module evaluates its enabled state at start and on an
  observed endpoint, not when an interface address appears. astrald keeps its `-root` and
  identity.

The node stays in the root netns and keeps its 10.77 address. The verifier asserts that
`ip.public_ip_candidates` lists the alias, which arms the `nat` module.
