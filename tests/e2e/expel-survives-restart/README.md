# expel-survives-restart

The User has banned node3 from a three-node swarm, then all three nodes
restart, and the ban holds: node3 is still out of every roster and cannot link
back in, and the two remaining members form the swarm again.

- **after** `expel-propagation` (the end state of that test: node3 expelled
  by the User on node1, node1 and node2 hold the ban, node3 is `unclaimed`) ·
  **restart** `node1`, `node2`, `node3` · **destroys** — a restart replaces
  every node's process.
- **driver** `script.py` — changes nothing. It waits until the ban has
  settled (node1 and node2 hold the User's ban on node3 and list only node1
  and node2; node3 reads `unclaimed`), then records the remaining members and
  each node's restart count.
- **runner** — restarts the three nodes between driver and oracle.
- **oracle** `verify.py`, by its own reads —
  - each node was restarted exactly once;
  - node1 (User token) and node2 (own token): `user.list_expelled` is exactly
    the User's ban on node3; within 60 s `user.swarm_status` lists exactly
    node1 and node2, each marks the other linked, and `user.list_siblings`
    lists exactly the other one;
  - node3: `user.info` answers `unclaimed` — not `claimed` (its cleared
    contract stayed cleared) and not `fresh` (the ownership history and the
    manager stayed, `mod/sys/src/state.go`) — and its own gate denies
    `user.swarm_status`;
  - node3's `user.join` to node1 and the User's `user.adopt` of node3 are
    refused with "identity expelled from swarm"; afterwards neither roster has
    node3 and node3 is still `unclaimed`.

Bans are stored in the user module's database (`dbExpulsion`), and
`expel-propagation` already proves they reach node2 and node3; this test is
the part that a restart could lose: a ban read only from memory, an active
contract that returns, or a roster that re-admits the banned node.

## Place in the plan

`expel-propagation` continues `swarm-roster-sync`'s world, and this test
continues `expel-propagation`'s, so all three share one world. It is a
destroyer because it restarts every node; a test that follows it needs the
expelled, restarted swarm as its start.

## Why the world stops every node before it starts any

`restart = [node1, node2, node3]` used to restart the nodes one after the
other, each while its siblings still ran. A node that came back without its
bans relearned them: the siblings push every ban they hold to a freshly linked
peer (`syncExpulsions`, `mod/user/src/sync.go`), so a ban that no node loaded
from storage was healed within a second, and the oracle could not tell.
`LocalWorld._restart` (`tests/lib/worlds/local.py`) and `DockerWorld.restart`
(`tests/lib/worlds/docker.py`) now stop all the named nodes, then start them
in order. A single-node restart behaves as before.
