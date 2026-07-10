# tor-link

A node automatically reconnects over Tor when it loses the local network.

- **Kind:** scenario · **Family:** network
- **Chain:** `two-nodes` → `two-nodes-tor`
- **Steps:** enable-tor · leave-lan · link-over-tor
- **Run:** `netsim story --stage two-nodes --save two-nodes-tor netsim/scenarios/tor-link/tor-link.story`

Two nodes start connected on a local network and both set up Tor. Then one node leaves the LAN entirely, and the scenario checks that the pair automatically re-links over Tor with no help. Tests that astrald finds an alternate path when the primary one disappears.
