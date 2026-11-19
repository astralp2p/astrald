# gateway-relink

A node reachable only through a gateway is relinked by its sibling's link
task, with no `nodes.new_link`: the basic strategy dials the sibling's gw row.

- **after** `gateway-relay` · **ops** `noop` (node2 behind NAT, registered with the reflector
  as its gateway; node1 holds node2's `gw:<reflector>:<node2>` row and a gw
  link to node2) · **destroys** `true` · **roster** `node1`, `node2`,
  `reflector`.
- **driver** `script.py` — closes every link node1 holds to node2, then only
  watches: polls node1's links to node2 every second until a new outbound gw
  link stands for 5 s, or 180 s pass, and taps node1's log for its link-task
  lines about node2. Calls no `nodes.new_link`.
- **oracle** `verify.py` — node1 held a gw link to node2 at the start; a new
  outbound gw link from node1 to node2 stood within 90 s of the close; node1's
  log shows its link task taking the link as broken and restoring it.

why outbound on node1: node2 holds no row that reaches node1 from behind NAT
(gateway-relay asserts that no direct tcp link forms), so the new link is
node1's dial, and the log names node1's link task as the dialer.

why it fails at astrald `463a4fea`: basic was registered for tcp only, and
`BasicLinkStrategy.Signal` dropped node1's gw row for node2 before a worker
saw it, so the link task ended in `link not produced` and backed off.
