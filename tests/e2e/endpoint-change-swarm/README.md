# endpoint-change-swarm

The world the endpoint-change tests start from: three siblings of one User,
`node2` (A) behind a NAT that admits no inbound connection, `node1` (B) and
the `reflector` (C) on the LAN, every pair linked.

- **after** `adopt-node` (node1 and node2 linked siblings) · **roster**
  `node1`, `node2`, `reflector` · VM world.
- **op** `enter-nat node2 --translating`: node2's astrald moves into a netns
  behind a NAT with no inbound DNAT and new inbound UDP dropped, and restarts
  there; node2 relinks to node1 from behind it.
- **driver** `script.py`: the reflector and node1 learn each other's LAN
  endpoint and node2 is given none; as the User on node1 it links to
  the reflector and adopts it; it names the three nodes on each node; then it
  polls the three nodes' links and swarms every second until the mesh stands
  or 180 s pass.
- **oracle** `verify.py`: the User adopted the reflector; node2 holds a link
  to node1 and to the reflector, and every link node2 holds is outbound;
  node1 holds a link to the reflector; each node's swarm lists the other two.

The test judges the setup only. `endpoint-change-relink` and
`endpoint-change-double` continue its world.

node2 is given no endpoint of the reflector. The world has no gateway and no
Tor on purpose: node2 behind its NAT cannot be dialed and cannot dial an
endpoint it does not hold. The reflector's endpoint reaches node2 through the
first-hand pull of the reflector's endpoints and the forwarded
`EndpointsChangedMessage` (A3G14, EP5), so this test is that path's regression
test: node2 links to the reflector within the driver's 180 s.
