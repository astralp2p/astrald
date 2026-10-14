# read-swarm

node1, acting as the User, reads an object that lives only on node2 without
naming node2. The finders name node2, and node1 reads from it.

- **env** node · **start** `two-nodes-data-peer` · **saves** nothing
- **driver** `script.py` — on node1 with no target: `objects.read` with
  `zone=dvn`, `objects.read` with `zone=dv`, `objects.find` with `zone=dvn`.
- **oracle** `verify.py` — node1's `dvn` read returns node2's own copy, the
  `dv` read is rejected, and `objects.find` names node2.

`zone=dv` keeps the read on node1, which does not hold the object, so the
read is rejected. `zone=dvn` lets the swarm finder ask node2, and node1 reads
from it as itself.
