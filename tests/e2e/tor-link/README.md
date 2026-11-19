# tor-link

Story 0004. node2 drops off the LAN and the pair re-links over Tor.

- **env** netsim — the point is the network, not the daemon.
- **start** `two-nodes` · **saves** `two-nodes-tor`
- **steps** `enable-tor` on both, `leave-lan` on node2, then the driver.
  No step closes node1's tcp link to node2: the LAN path black-holes, and
  node1's keepalive closes the link (astral-docs `topics/link-mux.md`,
  Keepalive).
- **driver** asks node2 for `user.info` from node1 until it answers, and
  records when; then holds a tor link to node2.
- **oracle** `verify.py` — node2 first answers within tcp's
  `link_idle_ping` + `link_ping_timeout` + one query (15 s + 10 s + 60 s,
  plus 2 s) of the driver's start, over tor; a tor link to the peer on
  **both** ends, and no surviving tcp link. Without the last assertion a tor
  link proves nothing: the LAN path might still be carrying the swarm.

The answer bound is ticket soYSZDACSo's acceptance (A1G2 option a): the dead
link closes within its transport's keepalive bound, and the next query goes
over a working link. Queries sent before the close may fail.

Tor comes up while the LAN link is still live, so the onions publish and sync
before the direct path is severed.
