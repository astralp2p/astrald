# expel-propagation

The User bans node3 from a three-node swarm, and the ban holds everywhere:
on the issuer, on a sibling that never saw the expel, and on the banned node
itself.

- **after** `swarm-roster-sync` (node1, node2, node3 in the User's swarm) ·
  **destroys** `true` — a ban cannot be lifted.
- **driver** `script.py` — the User stores a bait object on node1 and
  expels node3; node3, with its own token, waits until it no longer reads
  itself as `claimed`, then tries to read the bait from node1, to join
  node1's swarm, and to list its own repositories; the User signs node3 a new
  Privileged membership and offers it to node1 and node2. The outcomes are
  the `probes` fact, for the report only.
- **oracle** `verify.py`, by its own reads —
  - node1: `user.list_expelled` holds node3 issued by the User; the roster
    keeps node1 and node2 and drops node3; the bait is in node1's `local`;
  - node2, with its own token (`list_expelled` authorizes SeeSwarm, which
    admits members; no User token needs minting): the same ban, node3 gone
    from its roster, within 30 s;
  - node3: `unclaimed`, never `fresh` (astral-docs `user-and-swarm.md`,
    Losing the owner); its gate denies `objects.repositories`;
  - node1 refuses node3's links once node3 answered its ban (astral-docs
    `protocols/user/README.md`, Link admission), so node3 reaches node1 not
    at all: its bait read is rejected with code 7 (unavailable), its
    `user.join` fails to reach node1 (`user.join.md`: an `error_message` if
    the swarm node cannot be reached; "identity expelled from swarm" is
    also accepted, never a success), and node1 holds no link to node3;
  - the User cannot bring node3 back: the oracle has the User sign node3 a
    new Privileged membership on node1 (`crypto.sign_hash`) and offers it to
    node1 and node2 with `nodes.swarm_push`, and for 10 s neither roster
    lists node3. An expel beats everything signed for the expelled node
    (`mod/user/src/swarm_view_membership.go`). Before G7 this probe was the
    User's `user.adopt`, which is gone; a membership the User signs is what
    re-adoption is now;
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

Since link admission, node3's probes no longer open links to node1: node1
refuses them right after the Handshake, so the oracle asserts that node1
holds no link to node3.

## Also observed: node3 briefly refuses its own token

Between storing the ban on itself and clearing its contract
(`receiveExpulsion` → `leaveSwarm`), node3 still reads as claimed but is no
longer a member, so `user.info` answers its own node token "not permitted"
(`op_info.go`, SeeSwarm). Run `20261105T014140Z` hit it once in five, 1 ms
before `leaveSwarm` began. The driver and the oracle treat that answer as
the leave in progress and judge only the settled state, `unclaimed`.
