# endpoint-change-relink

A sibling behind NAT links to a sibling that moved to a new subnet within
30 s of the move, with no manual `nodes.add_endpoint`: the mover announces
its change to its linked siblings, the public sibling forwards the notice one
hop, and the NAT'd sibling pulls the new endpoints from it and wakes its link
task. Endpoint-change ticket, acceptance criterion 5.

- **after** `endpoint-change-swarm`: `node2` (A) behind a NAT that admits no
  inbound connection, `node1` (B) and the `reflector` (C) on the LAN, every
  pair linked · **roster** `node1`, `node2`, `reflector` · VM world.
- **op** `move-subnet node1 --to 10.78.0 --in 30`: 30 s later node1 takes
  `10.78.0.<octet>` and gives up its 10.77 address; every VM routes the new
  subnet, and node1 routes the LAN and the NAT aliases.
- **driver** `script.py` (`lib/addrmove.py` `record`): taps every node's log;
  records every node's links and the endpoints node2 resolves for node1;
  polls node1's own addresses every 0.25 s and, as soon as an address goes,
  closes on every node the links that rode it; then polls node2's links to
  node1 and the endpoints node2 and the reflector resolve for node1 until
  node2 has held an outbound link to the new address for 5 s and both resolve
  it, or 60 s pass. It gives no node an endpoint.
- **oracle** `verify.py`: before the move node1 held no new address, node2
  held a link to the reflector and resolved no row of node1 at the new
  address; node1 took the new address and gave up the old one; node2 lists an
  outbound link to node1 at the new address within 30 s of the driver seeing
  node1 take it; node2's log holds `pulled N new endpoints of <node1> from
  <reflector>`.

Why the driver closes the dead links: a withdrawn address leaves every link
that rode it pooled until each end's keepalive closes it, up to 25 s over
tcp, and a link task's wake does nothing while its target counts as linked.
The ticket leaves dead links to the keepalive tickets.

What happens before this change: node2 keeps dialing node1's old rows and
never learns the new one, since nothing pulls endpoints for a sibling that
moved while node2 stays linked to the reflector.

Not covered: node1's own announcement count, and a mover that lost every
link (the ticket's open questions).
