# tor-restart-onion

After Tor restarts under a running node, the node withdraws its onion while
Tor is down, registers the same onion again without a node restart, and a
sibling links to it over Tor.

- **after** `adopt-node` · **roster** `node1`, `node2` · VM world.
- **ops** `enable-tor node1 node2`: both nodes run Tor and publish an onion;
  `restart-tor node2 --in 60 --down 30`: 60 s later Tor stops on node2, and
  30 s after that it starts again. astrald runs throughout.
- **driver** `script.py`: taps node2's log; every 1 s reads the endpoints
  node2 resolves for itself (the tor row and its address), and notes when it
  first sees node2's `is down: tor control connection closed` line. Once a
  sample after Tor's start holds the tor row again, node1, as the User,
  dials `nodes.new_link(node2, endpoint=tor:<onion>)` every 10 s until it
  lists a tor link to node2, at most 240 s; then asks node2 for `user.info`
  and reads both ends' tor links.
- **oracle** `verify.py`:
  - the tor row was present before the stop;
  - every sample after the down line, until Tor's start, held no tor row;
  - the row was back within 75 s of Tor's start (startServer's 1 min backoff
    cap and 15 s), at the same address (`tor.key` is reused);
  - a tor link node1→node2 formed within 240 s of the row's return, both
    ends list it under one id, and node2 answered `user.info` while it was
    up;
  - node2's log tap stayed whole: astrald did not restart.

Tor's start is the down line's time on the host's clock plus `--down`:
restart-tor schedules the stop and the start in one call, and astrald logs
the loss as Tor's control connection closes. The oracle allows 2 s for the
tap's delay.

The query's route is not observable: node1 also holds its LAN link to node2,
and routing may carry `user.info` over it. The tor link, negotiated over the
onion and listed on both ends, is what shows the onion is reachable again;
`user.info` shows the pair answers while it is up.

The spec is silent on the onion's withdrawal and re-registration
(`mod/tor/src/server.go`, `mod/tor/src/endpoint_resolver.go`, commit
`8535c9cb`). Ticket: re-register the onion service after Tor restarts
(MVuHGbOzi5).
