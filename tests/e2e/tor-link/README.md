# tor-link

Story 0004. node2 drops off the LAN and the pair re-links over Tor.

- **env** netsim — the point is the network, not the daemon.
- **start** `two-nodes` · **saves** `two-nodes-tor`
- **steps** `enable-tor` on both, `leave-lan` on node2, then the driver.
- **oracle** `verify.py` — a tor link to the peer on **both** ends, and no
  surviving tcp link. Without the second assertion a tor link proves nothing:
  the LAN path might still be carrying the swarm.

Tor comes up while the LAN link is still live, so the onions publish and sync
before the direct path is severed.
