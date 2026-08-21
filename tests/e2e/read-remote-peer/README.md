# read-remote-peer

Story 0008. node1, acting as the User, reads over astral an object that
lives on node2.

- **env** node · **start** `two-nodes-data-peer` · **saves** `two-nodes-data-read`
- **driver** `script.py` — `objects.load(target=node2)` on node1.
- **oracle** `verify.py` — two independent loads, node1-routed-to-node2 and
  node2's own `local` repository, must return the same non-empty bytes.

The read is issued as the User. An anonymous query carries no network zone
and never routes to the peer.

No `payload.txt` of its own: the peer's copy is the ground truth, and the
object id is a content hash, so a second file would only be a second thing
to keep in sync.
