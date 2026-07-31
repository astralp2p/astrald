# expel-node

Story 0009. The User permanently bans node2 from the swarm.

- **env** node · **start** `two-nodes` · **saves** `two-nodes-expel`
- **mutates** `true` — suite-last, always.
- **driver** `script.py` — `user.expel(node2)` on node1 as the User.
- **oracle** `verify.py` — node2 must appear in `user.list_expelled` under
  the User as issuer, and must be gone from `user.swarm_status`.

The ban is identity-level and irreversible: astrald has no op that lifts
one. `two-nodes` therefore stops being consumable the moment this test runs,
which is what `mutates` tells the suite walk — ancestry alone cannot express
it, since `two-nodes` remains an ancestor of `two-nodes-expel`.

node2's identity comes from `session.json`, not from node2: an expelled node
refuses `user.info`, so it is not a usable identity source afterwards.
