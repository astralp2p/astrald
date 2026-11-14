# link-session-migrate-serving-side

`nodes.migrate_session(start=true)`, started on the serving node, moves a
live 32 MiB read onto a second link with no byte lost, duplicated or
reordered. On the serving node the session is relayed for the User: its
logical peer is the User, and the node across its link is the reader.

- **after** `adopt-node` (node1 and node2 adopted siblings, one link) ·
  **destroys** `true` — the world ends with a second link and a 32 MiB object
  on node2. It does not use `link-session-migrate`'s state.
- **driver** `script.py` — as the User on node1: stores a seeded 32 MiB
  object on node2 (`lib/blobgen.py`), opens a second link Lt to node2 at
  node2's `lan_endpoint`, and reads the object with a paced reader. At 8 MiB
  the reader holds, so the stream stands still with its session open. The
  driver records both nodes' rows of the session (link Ls), then calls
  `nodes.migrate_session(session, Lt, start=true)` on node2 with node2's own
  token, records the answer, its time and both nodes' rows again, releases
  the reader and reads to EOF. No link is closed. Judges nothing.
- **oracle** `verify.py` — first what holds whatever the migrate answered:
  the read did not stall and reached EOF; the bytes match the seed's
  regeneration and the hash in `object_id`, per MiB; before the move node1
  listed the session outbound to node2 and node2 inbound from the User,
  both on Ls; right after the answer both list it on one link, Lt after an
  ack and Ls after an error; the answer came within 45 s; within 10 s after
  the read no session of it is listed. Last, the contested claim: the
  migrate answered an ack.

why the hold: a migrate started while node2 writes at full speed would test
the race `link-session-migrate` covers. Held, node2's writer is blocked on
flow control, and the move itself is what is judged.

On master `9f647ff1` the migrate answered `route not found` after 30 s in
every run, and the stream stayed whole on Ls: `migrateSession` sent the
control query to the session's `RemoteIdentity`, the User, and no link leads
to the User (`mod/nodes/src/migrate_session.go`). It now addresses the node
across the session's link.
