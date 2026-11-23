# swarm-fallback-paths

Two siblings lose their LAN and reach each other over Tor; Tor restarts under
a running node and the pair reaches each other over Tor again without a node
restart; the LAN returns and every query keeps answering.

- **after** `adopt-node` · **roster** `node1`, `node2` · VM world.
- **ops**, in this order:
  - `enable-tor node1 node2`: both nodes run Tor and publish an onion while
    their LAN link is live, so the onions sync before the cut (as tor-link);
  - `restart-tor node2 --in 240 --down 20`: Tor on node2 stops 240 s after
    the op and starts 20 s later; astrald runs throughout;
  - `lan-outage node2 --for 480`: node2 leaves the LAN now, the links that
    rode its address are closed on every VM, and the address returns 480 s
    later, about 220 s after Tor's start: a LAN back within phase B's 180 s
    would carry the queries over tcp and leave Tor's recovery unjudged.
- **driver** `script.py`:
  - gives each node the other's onion where it holds none, and records it
    (`seeded`): the cut follows the onions' publication by seconds;
  - taps node2's log for the whole flow;
  - every 1 s per direction, one query at a time, 30 s deadline: node1 as
    the User asks node2 for `user.info`, and node2 with its node token asks
    node1; each record holds start, end, outcome class and, after an answer,
    the networks of the caller's links to the peer;
  - every 1 s: both nodes' links to each other, whether node2 resolves a tor
    row for itself, whether node2 holds its 10.77 address; the time it first
    sees node2's `is down: tor control connection closed` line;
  - ends 60 s after the LAN's return; it notes when node1 and node2 first
    share a tcp link after the return and when node1 then lists no tor link
    to node2.
- **oracle** `verify.py`:
  - phase A (LAN gone): node2 first answers node1 within 87 s of the
    driver's start, with node1 holding only tor links to node2;
  - phase B (Tor restart): node2 resolves no tor row for itself from the
    down line until Tor's start; queries in both directions answer over a
    tor link again within 180 s of Tor's start, and node2's address returned
    no sooner than that bound ran out; node2's log tap stays whole, so
    astrald did not restart;
  - phase C (LAN back): every query in either direction that started once
    node2's address returned answers; the tcp link and the tor link's retire
    are reported, not judged;
  - no query outlived its 30 s deadline by more than 2 s.

Bounds: phase A is tor-link's (tcp `N + T` 25 s, one 60 s query, 2 s;
soYSZDACSo). Phase B adds the 1 min re-register backoff cap
(`mod/tor/src/server.go`, commit `8535c9cb`), tor's keepalive bound of 60 s
(astral-docs `topics/link-mux.md`, Keepalive, Per-transport values) and 60 s
for a dial. Phase C's 60 s window covers the 5 s announce wait and one 20 s
pull that node2's return starts (astral-docs `protocols/nodes/README.md`,
Announcing a change). The spec is silent on the onion's withdrawal; that bound
follows the code.

Phase C judges no move back to tcp. A node wakes its link to a member only
when it is not linked to that member (astral-docs `protocols/user/README.md`),
and a connectivity upgrade follows link pressure only
(`mod/nodes/src/connectivity_upgrade.go`), so nothing dials tcp while the tor
link stays up. The first netsim pass (2026-11-23) saw the pair stay on the
tor link for 230 s after the LAN's return, with every query answered.
Whether a node should move a linked member to a higher-ranked transport when
one returns is 3XBBRfmcTC's open question 1.

Tor's start is the down line's time on the host's clock plus `--down`:
restart-tor's one unit stops Tor, sleeps `--down` s and starts it. The LAN events
and the links happen on different guests, so every time is the host's.

Apps are not judged: a VM world runs no host app, so the pair is judged with
node-level queries both ways (operator decision Q2, 2026-11-23). The gateway
path is `gateway-relay`'s.

Tickets: MVuHGbOzi5 (onion re-register), soYSZDACSo (route over a working
link), XvgjSFDKa9 and AOcdNCbukM
(wake links on address changes); reports sygjmtS9fy and 9D8viEzJpS.
