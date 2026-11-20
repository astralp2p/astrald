# kcp-restart-stale-link

Ticket akchpFNjbO criterion 7 (S5 on kcp): a kcp-only sibling is killed and
starts again, and its peer drops the stale kcp link within 35 s and reaches
it again without `nodes.close_link`.

- **after** `kcp-only-relink` (node1 and node2 joined by kcp alone, tcp
  between them dropped for good) · **roster** `node1`, `node2` · VM world.
- **ops** `restart-astrald node2 --in 30 --kill` (node2's astrald is killed
  30 s later and systemd starts it again; a killed node sends node1 nothing
  as it goes, so node1 finds the old link dead on its own).
- **driver** `script.py` — polls every 0.5 s: node1's links to node2 while
  node2 still answers (the last list is the stale links), when node2 stops
  answering and answers again, and node1's links to node2 after that, until
  node1 lists none of the stale links and a new one, or 60 s pass. Judges
  nothing and closes no link.
- **oracle** `verify.py` — the restart took; before it node1 held links to
  node2, all kcp; node1 listed none of the stale links within 35 s of node2
  going down (kcp `link_idle_ping` 15 s plus `link_ping_timeout` 10 s, plus
  slack) and lists none now; a query from node1 to node2 as the User
  (`user.info`) is answered.

The drop comes from the keepalive, or sooner from the probe a new link
sends over the older links to its peer (mod/nodes/src/link_pool.go
`probeOlderLinks`). The bound runs from `down`, the first poll node2 did not
answer: the old process's links die at the kill, and the keepalive counts
from the last frame received.

Distinct from `kcp-only-restart-relink` (`main.suite`, local or Docker),
which judges the same drop from its oracle's start after a runner restart;
that test fails at astrald `66f3e651` (ticket HOGA1CxQDP), so this one may
fail on the query until that fix lands.
