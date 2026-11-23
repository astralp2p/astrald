# endpoints-advertised-clean

A node with a container bridge up advertises neither the bridge's address
nor a zone-less `fe80::` address, on the node itself and on its sibling; its
LAN endpoints remain with the private TTL, and a configured public endpoint
is advertised with the public TTL.

- **after** `adopt-node` · **roster** `node1`, `node2` · VM world: a local
  node advertises no interface address (`tests/lib/localnode.py`), so there
  is nothing to filter there.
- **op** `add-bridge node2 --name docker0 --cidr 172.17.0.1/16`: node2 holds
  Docker's default bridge address on a bridge named as Docker names it.
- **config** `[config.node2.tcp] configEndpoints = ["203.0.113.5:40000"]`: a
  manual public endpoint, in TEST-NET-3 so nothing answers it.
- **driver** `script.py`: waits 10 s (the ip module polls every 3 s, an
  address change starts the 5 s announce wait); reads node2's
  `ip.local_addrs` and the endpoints node2 resolves for itself; node1 closes
  its links to node2 and links again, so it pulls node2's endpoints from
  node2 over its only link; polls node1's rows for node2 until the public and
  the LAN kcp rows are written after the new link, at most 30 s; reads
  node1's links to node2 with both ends. The LAN tcp row cannot date the
  pull: adopt-node wrote it on node1 with `nodes.add_endpoint`, and no pull
  moves the expiry of such a row. The facts keep each
  `nodes.resolve_endpoints` answer as the stream it was sent in. node1's rows
  keep the longest TTL per endpoint, since nearby lists node2's LAN
  endpoints a second time with at most 300 s, and the order of resolvers
  never decides a key.
- **oracle** `verify.py`:
  - control: node2 holds `172.17.0.1` and an `fe80::` address;
  - neither node2's own rows nor node1's rows for node2 hold an endpoint in
    `172.17.0.0/16` or `fe80::/10`;
  - both list node2's LAN tcp and kcp endpoints with a TTL of at most
    7200 s, and `tcp:203.0.113.5:40000` with a TTL above 7200 s; node1's
    LAN tcp row is exempt from the 7200 s bound, since adopt-node's manual
    row starts at 90 days and keeps its expiry, so node1's kcp rows judge
    the private class there;
  - node1's rows were written after the new link, and node1 holds no link
    with an end in either range;
  - node2's own answer is judged object by object and holds no endpoint
    twice: a node's own identity resolves to its listener endpoints only.

The test depends on the astrald fix on `bugfix/own-endpoints-listener-only`.
Before it, node2 stores node1's view of node2 (pulled when node1 forwards
node2's contract) and resolves those stored rows for itself next to its
listener endpoints; node1's first-hand pull then stores nearby's 300 s copy
of node2's LAN kcp endpoint, and the pull cannot be dated (run
20261123T121930Z, w6).

Sources: astral-docs `protocols/nodes/ops/nodes.resolve_endpoints.md`,
Returned objects (a node's own identity resolves to its listener endpoints;
nearby adds a peer's tcp and kcp endpoints with at most 300 s);
`protocols/nodes/README.md`, Announcing a change ("A
node never advertises an IPv6 link-local address"), Endpoint exchange in
the local swarm (the pull on a first link) and Withdrawn endpoints ("No pull
moves the expiry of a row that `nodes.add_endpoint` wrote earlier"); `protocols/tcp/README.md`,
configEndpoints (7 days for a configured entry, 2 h for an interface
endpoint on a non-public address). The spec is silent on container bridges:
the oracle follows `mod/ip/src/module.go` `LocalIPs` and
`mod/ip/src/advertisable.go`.

Tickets: bridge and link-local addresses (26lvYNBfHY), manual public
endpoint (Mn1sLkhVbc), and the private TTL class of sSsUQnIfMr. The CGNAT
ranges of sSsUQnIfMr stay unit tested, since the lab holds none.
