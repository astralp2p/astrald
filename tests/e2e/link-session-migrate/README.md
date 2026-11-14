# link-session-migrate

`nodes.migrate_session(start=true)`, started on the reading node, moves a
live 32 MiB read onto a second link with no byte lost, duplicated or
reordered; both nodes then report the session on the new link, and closing
the old link leaves the read whole.

- **after** `adopt-node` (node1 and node2 adopted siblings, one link) ·
  **destroys** `true` — the world ends with the adopt-node link closed, a
  second link in its place and a 32 MiB object on node2.
- **driver** `script.py` — as the User on node1: stores a 32 MiB object on
  node2, generated from a seed (`lib/blobgen.py`); opens a second link Lt to
  node2 at node2's `lan_endpoint`; opens `objects.read` of the object on node2
  and reads it with a paced reader (64 KiB reads, 8 ms apart, 15 s stall
  timeout, a sha256 at every MiB). At 8 MiB it takes the stream's session
  from node1's and node2's `nodes.sessions` (link Ls) and calls
  `nodes.migrate_session(session, Lt, start=true)` on node1. Right after the
  ack it records both nodes' session rows, node1's link byte counters, and
  the answer of a second migrate to Lt. After 2 MiB more it closes Ls on
  node1, then reads to EOF. Judges nothing.
- **oracle** `verify.py` — regenerates the 32 MiB from the seed, checks it
  against the hash and size in `object_id`, and compares the driver's length,
  final digest and per-MiB running digests with its own; a mismatch names
  the first differing MiB. The read did not stall (longest gap under 10 s)
  and reached EOF. The migrate started between 4 MiB and 8 MiB before the
  end and answered an ack. Before it both nodes listed the session on Ls,
  right after it both list it on Lt. The second migrate answered
  `session already on link`. Ls carried at most 256 KiB between the ack and
  its close; Lt carried at least 2 MiB more than before the migrate. Its own
  reads: Lt held by both nodes with `outbound` flipped, Ls by neither, and
  within 10 s no session of the read listed on either node.

why a seed: the oracle judges the stream against bytes it made itself, and
the hash in `object_id` proves those are the bytes node2 stored. The
driver's per-MiB digests are the one input the oracle does not own.

why both nodes' rows right after the ack: a session that is migrating is not
listed (astrald `mod/nodes/src/op_sessions.go`), and by verify time the read
is over.

On master `9f647ff1` the read lost data in 11 of 20 runs: the responder
resumes writing on Lt as soon as it reads `resume`, while the initiator
moved the session onto Lt's mux only after `done`
(`mod/nodes/src/migrate_session.go` `initiateMigration`). node1's Lt mux
answered the first data frames with a Reset, the frames were dropped, and
node2 closed the session. The initiator now attaches the session to Lt
before it sends `resume`.
