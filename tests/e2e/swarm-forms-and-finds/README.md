# swarm-forms-and-finds

Three fresh nodes of one User form one swarm by themselves: two on one LAN
meet through nearby with nothing given by hand, and the third, on another
subnet, joins through `user.introduce` with the one member endpoint the spec
requires. One node holds a container bridge, and no node advertises or
dials a bridge or zone-less link-local address. Every roster agrees.

- **root** test · **roster** `node1`, `node2`, `reflector` · VM world: the
  three nodes must be fresh, unclaimed and unlinked, and nearby needs a LAN.
- **ops**
  - `add-bridge node2 --name docker0 --cidr 172.17.0.1/16`: node2 holds
    Docker's default bridge address;
  - `move-subnet reflector --to 10.78.0 --in 30`: 30 s later the reflector
    takes `10.78.0.<octet>` and gives up its 10.77 address; every VM routes
    the other's subnet over the one LAN segment.
- **driver** `script.py`:
  1. waits until the reflector holds its 10.78.0 address;
  2. claims node1 with `user.bootstrap` and generated words, records the
     links node1 and node2 share, then claims node2 and the reflector with
     the same words; node2's claim starts the clock;
  3. polls links every 2 s, at most 240 s, until node1 and node2 share one;
  4. on the reflector, as the User: `nodes.add_endpoint(node1, <node1's LAN
     endpoint>)` and `user.introduce(node1)`; polls at most 90 s until the
     reflector shares a link with node1 and with node2;
  5. reads every node's endpoints for each of the three, its links with
     both ends, and `user.swarm_status` until it lists the three with the
     other two linked, at most 30 s.

  Every node's log is tapped for the nearby contact lines.
- **oracle** `verify.py`:
  - the reflector moved; one User claimed all three; node1 and node2 shared
    no link before node2's claim (the claim broadcasts node2's status at
    once, so they may link before it returns);
  - node1 and node2 linked within 200 s of node2's claim, with no endpoint
    given (two nearby status intervals of at most 95 s and 10 s for the
    dial), and node1 or node2 logged its nearby contact with the other;
  - `user.introduce` answered `ack`; the reflector linked to node1 within
    30 s of the answer and to node2 within 60 s of it, with no endpoint of
    node2 given;
  - no node lists a `172.17.0.0/16` or `fe80::/10` endpoint for any node,
    itself included, and no link has an end in either range;
  - every `user.swarm_status` lists exactly the three, the other two linked.

Sources: astral-docs `protocols/nearby/README.md` (a status every 85 to 95
s); `topics/user-and-swarm.md`, Replacing a node (a node that shares no
local network joins through `user.introduce` after one member endpoint is
added); `protocols/user/ops/user.introduce.md` (30 s push wait);
`protocols/nodes/README.md`, Announcing a change ("A node never advertises an IPv6 link-local address"). The spec is
silent on container bridges: the oracle follows `mod/ip/src/module.go`
`LocalIPs` and `mod/ip/src/advertisable.go`. A new member learns another
member's endpoints by a pull from a linked member once SwarmState sync lists
it, each pull bounded to 20 s (astrald `mod/user/src/module.go:165-182`,
`mod/nodes/src/endpoints_changed.go:173-217`).

The wiki flow gives no node an endpoint by hand. The spec requires one
member endpoint for a node that shares no local network, so the reflector
gets node1's, and nothing more (spec conflict C5 of the build plan).

Whether node1 and node2 ignore the reflector's broadcasts to its own subnet
on the shared segment is not known. If they hear them, the reflector meets
them through nearby before the introduction; the oracle accepts that case
and prints it.

Tickets: 26lvYNBfHY (bridge and link-local addresses), zdolWHVz9x
(first-hand forward), IiDlv0Ksyz (endpoint exchange inside the swarm),
g6pAQZTYlY (nearby listen ports), the lineage of 98SVlhAVHt, and the related
qwKe7aUYE2 and EOr06tN86x.
