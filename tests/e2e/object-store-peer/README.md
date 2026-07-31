# object-store-peer

Story 0007. node1, acting as the User, stores a payload **on the peer**. The
write is routed to node2, so the object lands in node2's repository.

- **env** node · **start** `two-nodes` · **saves** `two-nodes-data-peer`
- **driver** `script.py` — `objects.create(target=node2)` on node1.
- **oracle** `verify.py` — node2, queried directly with its own token, must
  contain the object in its `local` repository and return `payload.txt`
  byte for byte.

Its own directory rather than a parameter on `object-store`: one test, one
directory. Its `payload.txt` differs from `object-store`'s on purpose —
object ids are content hashes, so identical payloads would give the two
tests one id and let a local copy satisfy a peer-side check.
