# endpoint-change-double

A node whose address changes twice within 5 s gets its final address to its
siblings: the 5 s announcement wait that the first change starts takes in the
second, or the second starts a wait of its own, and either way the siblings
pull the address set as it stands. Endpoint-change ticket, acceptance
criterion 6.

- **after** `endpoint-change-relink`: `node1` (B) at `10.78.0.<octet>`,
  `node2` (A) behind NAT linked to it, the `reflector` (C) linked to both ·
  **roster** `node1`, `node2`, `reflector` · VM world.
- **op** `move-subnet node1 --to 10.79.0 --to 10.80.0 --gap 3.5 --in 30`:
  30 s later node1 moves to `10.79.0.<octet>`, and 3.5 s after that to
  `10.80.0.<octet>`. 3.5 s is under the 5 s wait and over mod/ip's 3 s poll,
  so astrald sees the intermediate address as a change of its own.
- **driver** `script.py` (`lib/addrmove.py` `record`): taps every node's log;
  polls node1's own addresses every 0.25 s until it holds the final one,
  closing on every node the links that rode each address it saw go; then
  polls node2's links to node1 and the endpoints node2 and the reflector
  resolve for node1 until node2 has held an outbound link to the final
  address for 5 s and both resolve it, or 60 s pass. It gives no node an
  endpoint.
- **oracle** `verify.py`: the driver saw node1 take the intermediate address
  and the final one less than 5 s later; node1's log holds two `network
  addresses changed` lines naming the intermediate address; node2 and the
  reflector each resolve node1 at the final address within 30 s of node1
  taking it, and still do at the end; node2 lists an outbound link to node1
  at the final address within 30 s.

The criterion names no bound; the oracle reuses criterion 5's 30 s. It
counts no announcements: whether mod/ip reports the second change inside the
first wait depends on where its 3 s poll falls, and either count delivers the
final set.

Not covered: a row at the intermediate address may stay on a sibling, since
removed addresses are not withdrawn (A3G5, deferred); it ages out with its
7 d TTL.
