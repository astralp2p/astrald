# expel-propagation

The User bans node3 from a three-node swarm, and the ban holds everywhere:
on the issuer, on a sibling that never saw the expel, and on the banned node
itself.

- **after** `swarm-roster-sync` (node1, node2, node3 in the User's swarm) ·
  **destroys** `true` — a ban cannot be lifted.
- **driver** `script.py` — the User stores a bait object on node1 and
  expels node3; node3, with its own token, waits until it no longer reads
  itself as `claimed`, then tries to read the bait from node1, to join
  node1's swarm, and to list its own repositories; the User tries to adopt
  node3 again. The outcomes are the `probes` fact, for the report only.
- **oracle** `verify.py`, by its own reads —
  - node1: `user.list_expelled` holds node3 issued by the User; the roster
    keeps node1 and node2 and drops node3; the bait is in node1's `local`;
  - node2, with its own token (`list_expelled` authorizes SeeSwarm, which
    admits members; no User token needs minting): the same ban, node3 gone
    from its roster, within 30 s;
  - node3: `unclaimed`, never `fresh` (astral-docs `user-and-swarm.md`,
    Losing the owner); its gate denies `objects.repositories`; node1
    refuses it the bait and refuses its `user.join` with "identity expelled
    from swarm";
  - node1 refuses the User's re-adoption of node3 the same way
    (`mod/user/src/contracts.go`, `IssueMembership`);
  - control: node2 is still linked to node1 and reads the same bait.

## Suspected astrald defect: a sibling re-links to the banned node

Link state between node2 and node3 is reported (`note: node2 holds N
link(s) to expelled node3` in `verify.log`), never asserted.
`applyExpulsion` (`mod/user/src/expel.go:90-100`) is meant to sever ties to
a banned peer, yet node2 can end up holding a link to node3 that nothing
closes again. The likely cause: node3 receives its ban first and drops its
links (`leaveSwarm`, `:108-119`); node2's `maintain_link` for node3 sees the
link close and schedules an ensure-link task
(`maintain_link_task.go:65`); `Scheduler.Schedule` takes no context, so the
cancel in `removeSibling` does not stop that task, and it links after
`applyExpulsion`'s `CloseLinks`. node3, now unclaimed, accepts the link.

Reproduction: `./tests/run expel-propagation --keep`, then node2's
`nodes.links`. In run `20261105T013725Z`, node2's astrald log (aliases
replaced by roster names):

    01:37:29.758  closed out-link with node3: EOF        (node3 left the swarm)
    01:37:29.758  link to node3 is broken, trying to reconnect
    01:37:29.773  added out-link with node3 (tcp)        (twice)
    01:37:29.782  removing sibling node3                 (the ban arrived)
    01:37:29.784  closed out-link with node3: node expelled
    01:37:29.819  added out-link with node3 (tcp)        (twice; one survives)

and node2 still held that link 24 s later. Earlier live runs saw the same
in 2 of 4 (a re-link ~5 ms after node3 tore its links down, alive 18 s).
Asserting "no link" would make this test red about half the time.

The probes themselves, the driver's and the oracle's, open fresh links from
node3 to node1, so a link node3–node1 says nothing about the ban either.

## Also observed: node3 briefly refuses its own token

Between storing the ban on itself and clearing its contract
(`receiveExpulsion` → `leaveSwarm`), node3 still reads as claimed but is no
longer a member, so `user.info` answers its own node token "not permitted"
(`op_info.go`, SeeSwarm). Run `20261105T014140Z` hit it once in five, 1 ms
before `leaveSwarm` began. The driver and the oracle treat that answer as
the leave in progress and judge only the settled state, `unclaimed`.
