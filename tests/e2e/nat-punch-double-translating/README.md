# nat-punch-double-translating

X06 Step 2. Two nodes, each behind a **port-translating, endpoint-independent,
port-restricted NAT**, punch through both NATs and get a kcp link that carries
traffic.

- **env** netsim · **after** `adopt-node` · **destroys** its world
- **steps** `enable-tor node1 node2`, `enter-nat node1 node2 --translating`,
  `configure-nat-tor node1 node2`,
  `add-reflector node1 node2 --reflector reflector --grant`, then the driver.
- **oracle** `verify.py`:
  - `nat.punch` from node1 to node2 returns a hole within **1 s**;
  - on both sides of that hole the bind port differs from the external port
    (both NATs translated: `40000+n` leaves as `20000+n`);
  - `nodes.new_link -strategies nat` yields a **kcp** link on both ends;
  - with tcp and tor dialing off and every other link closed, that kcp link
    carries **60 s** of round trips from node1 to node2 with **no loss after
    the first second**.

Each NAT admits only replies to a flow its node opened, so a node must send
to the exact external port of its peer. Each side reflects its punch socket
off the public `reflector` VM with `nat.reflect_udp` and advertises the
reflected endpoint; the peer's probes then reach a mapping that exists, and
its own probes open the mapping the other side's probes need.

The punch signals ride Tor (as in `nat-punch`): neither node reaches the
other before the punch. `--grant` makes the reflector grant both nodes
`mod.auth.admin_network_action`, the action `nat.reflect_udp` answers to; the
reflector belongs to no swarm.
