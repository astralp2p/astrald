# nat-punch

Two NAT'd peers hole-punch to a direct connection, coordinating over Tor.

- **Kind:** scenario · **Family:** network
- **Chain:** `two-nodes` → `two-nodes-nat`
- **Steps:** add-vm · install-astrald · enable-tor · enter-nat · configure-nat-tor · add-reflector · punch-nat
- **Run:** `netsim story --stage two-nodes --save two-nodes-nat netsim/scenarios/nat-punch/nat-punch.story`

Both nodes are put behind their own NAT so they have no direct path to each other. A public reflector node tells each one its outside address, and they use a Tor link to coordinate a simultaneous connection attempt. On success the pair ends up talking over a direct kcp link instead of relaying through Tor.

> **Status:** works end to end (direct kcp link verified on both peers). The simulated NAT is currently a permissive *full-cone* NAT; a stricter, filtered NAT that a punch must genuinely defeat is under evaluation.
