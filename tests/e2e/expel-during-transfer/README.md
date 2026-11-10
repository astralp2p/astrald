# expel-during-transfer

The User bans node2 while node2 is reading a large object from node1, and
node2 cannot read again.

- **after** `adopt-node` (node1 and node2 in the User's swarm) ·
  **destroys** `true` — a ban cannot be lifted.
- **driver** `script.py` — the User stores a 32 MiB object on node1; node2,
  with its own token, opens a streaming read of it from node1 and takes the
  first megabyte at about 5 MB/s; the User expels node2 with the stream open;
  node2 keeps reading until the stream ends, breaks or stalls. That outcome is
  the `transfer` fact.
- **oracle** `verify.py`, by its own reads —
  - the ban was issued mid-read: some, and not all, of the object had
    arrived before `user.expel`;
  - node1: `user.list_expelled` holds node2 issued by the User; the roster
    drops node2; the object is still in `local`; control: the User reads its
    first bytes;
  - node2: settles as `unclaimed` within 30 s, so the ban reached it;
  - a new read of the object by node2 from node1 is a `QueryRejected` from
    node1 (not a timeout, not an unreachable route, not a local denial).

## Open question: may a stream authorized before the ban finish?

Not asserted. astral-docs (`topics/user-and-swarm.md`, `user.expel`) say the
ban is "for good" and that the node's contract is cleared, and say nothing
about a stream the member opened earlier. In astrald, a query is authorized
once, when it arrives (`mod/user/src/authorizers.go`, `AuthorizeSeeObjects`
over `LocalSwarm`, which excludes the expelled); nothing re-checks a running
stream, and node1 does not close links to the subject on `Expel`
(`expel.go`). node2, on receiving the ban, closes its links to its former
siblings (`leaveSwarm`), which takes the stream down from node2's side. So
whether the transfer ends cut or complete depends on node2 cooperating, and
a node that ignored the ban would keep the stream. The `transfer` fact and the
oracle's `note:` line in `verify.log` report what this run did: `ended`,
`received`, `complete` and `after_ban_s`.

Observed on this host: with the ban reaching node2, the stream ends in a
plain EOF after 5 to 11 MB of the 32 MiB (cut within about 2 s of the ban,
without an error: a client that does not know the size cannot tell it from a
finished read). With the push to node2 removed from `Expel`, the same stream
ran to the full 32 MiB, so node1 itself never cuts it.
