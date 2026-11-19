# nat-punch-translating

X06 Step 1. A node behind a **port-translating, endpoint-independent NAT** punches
to a **public peer** and gets a kcp link that carries traffic.

- **env** netsim · **after** `adopt-node` · **destroys** its world
- **steps** `enter-nat node1 --translating`, `go-public node2`,
  `add-reflector node1 --reflector node2`, then the driver.
- **oracle** `verify.py`:
  - `nat.punch` from node1 to node2 returns a hole within **1 s**;
  - node1's side of that hole binds a local port that differs from the external
    port node2 saw (the NAT translated it: `40000+n` leaves as `20000+n`);
  - `nodes.new_link -strategies nat` yields a **kcp** link on both ends;
  - with tcp and tor dialing off and every other link closed, that kcp link
    carries **60 s** of round trips from node1 to node2 with **no loss after
    the first second**.

Before the fix the participant dropped every probe from the translated port,
because it accepted only the announced port plus or minus 10, and a punched
hole bound the external port instead of the local one.

node2 is the public peer: `go-public` gives it a `198.51.100.<oct>` alias and
restarts it so its `nat` module arms. `add-reflector` with node2 as the
reflector links node1 to node2 over tcp; node2 reflects node1's public alias,
which arms node1's `nat` module, and the tcp link carries the punch signals.

The echo is a remote `nodes.links` query from node1 to node2 every 0.25 s.
