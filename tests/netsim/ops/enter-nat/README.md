# enter-nat

Puts a node's astrald behind its own **symmetric, true-masquerade NAT** — the `leave-lan`
analog for the NAT tests.

On each `--vm` (default `node1 node2`):

- creates netns **`priv`** holding the private host `192.168.99.2`, wired to the VM by a
  `veth` pair (`192.168.99.1` on the VM side);
- installs a **port-preserving SNAT** of `192.168.99.0/24` to a per-node public TEST-NET
  alias **`198.51.100.<lan-octet>`** on the LAN NIC (`lan0`) — validated as
  endpoint-independent (cone) by `nat-eim-probe`;
- relaunches astrald **inside the netns** via a systemd drop-in
  (`NetworkNamespacePath=/run/netns/priv`), which joins only the *network* namespace, so
  the apphost unix socket stays in the shared mount namespace and `astral-query` still
  reaches it from the root namespace. astrald keeps its `-root` and identity.

With **`--translating`** the NAT is a port-translating, endpoint-independent NAT with
address-and-port-dependent filtering, the class of Starlink's NAT (X06):

- the netns hands out local ports from `40000-40999` only, and an nft map sends UDP from local
  port `40000+n` out from external port `20000+n` toward every destination (one mapping per
  socket, never the local port itself; Linux's `snat ... random` would pick a port per flow,
  which is a symmetric NAT);
- there is no inbound DNAT: conntrack admits only replies to a flow the node opened, and new
  inbound UDP to the alias is dropped at the VM's `input` (table `e2e_nat_filter`);
- other traffic, TCP included, keeps the port-preserving SNAT.

With **`--peer <host>`** (repeatable) each named LAN VM routes each `--vm`'s public alias
`198.51.100.<lan-octet>/32` over its LAN NIC, so it can answer the dials the NAT'd node makes
to it. Without the route the peer's replies leave by its default route (the user-mode NIC) and
every such dial hangs. The peer gets only the `/32`: no alias, so it gains no public IP
candidate, and no route to the `/24`, so a NAT'd node the call does not name stays unanswered
(`gateway-relay` relies on that, see `configure-gateway` step 0b). A peer that already holds a
`198.51.100` alias routes the `/24` on-link and is left as it is.

Moving astrald off the flat `10.77` address withdraws its direct LAN endpoint (astrald
polls `InterfaceAddrs`; in the netns it sees only `192.168.99.2`), so the pair re-links
over Tor — exactly the `leave-lan` dynamic.

**This task only builds the NAT; it does not punch.** astrald cannot see its own public
alias (that is what masquerade means), so its `nat` module stays **disabled** until the
`reflector` node reflects that endpoint back — see `add-reflector`. The pre-punch
milestone is: after `enter-nat` + `add-reflector`, `nat` reports **enabled** on both peers.

## Notes / follow-ups

- The netns currently routes only to the LAN (for reflection and, later, the peer punch).
  The **punch increment** will additionally need the netns routed to the WAN (slirp) so
  the Tor signaling link can form from inside the netns — add a `masquerade` rule for the
  WAN NIC then.
- Verify via `astral-query` (unix socket, reachable from the root ns) or
  `ip netns exec priv astral-query …`; the apphost WS port now lives inside the netns.
