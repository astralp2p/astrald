# tor-link

Story 0004. node2 drops off the LAN and the pair re-links over Tor.

- **env** netsim — the point is the network, not the daemon.
- **start** `two-nodes` · **saves** `two-nodes-tor`
- **steps** `enable-tor` on both, `leave-lan` on node2, then the driver.
  No step closes node1's tcp link to node2: the LAN path black-holes, and
  node1's keepalive closes the link (astral-docs `topics/link-mux.md`,
  Keepalive).
- **driver** asks node2 for `user.info` from node1 until it answers, and
  records when; then holds a tor link to node2. A watcher polls node1's
  links to node2 every second: the old links (node1's non-tor links at the
  first poll), the new link, and from the new link on, node1's endpoint
  rows for node2 against the endpoints node2 resolves for itself.
- **oracle** `verify.py` — node2 first answers within tcp's
  `link_idle_ping` + `link_ping_timeout` + one query (15 s + 10 s + 60 s,
  plus 2 s) of the driver's start, over tor; node1 lists none of its old
  links to node2 within 35 s of the new link; within 60 s of the new link,
  node1's endpoint table holds every tcp, kcp and tor endpoint node2
  advertises, each written after the new link (its time left is at least
  node2's TTL less the time since the new link, less 5 s); a tor link to
  the peer on **both** ends, and no surviving tcp link. Without the last
  assertion a tor link proves nothing: the LAN path might still be carrying
  the swarm.

The old-link drop and the endpoint table are ticket akchpFNjbO's criterion 6,
netsim half: a new link probes the older links to its peer, and the endpoint
pull runs on the first link and on a partial close. An operator-driven run
records no link watch (`prompt.md` asks for `tcp` as an empty object), so the
oracle judges those two on scripted runs alone and says so.

The answer bound is ticket soYSZDACSo's acceptance (A1G2 option a): the dead
link closes within its transport's keepalive bound, and the next query goes
over a working link. Queries sent before the close may fail.

Tor comes up while the LAN link is still live, so the onions publish and sync
before the direct path is severed.
