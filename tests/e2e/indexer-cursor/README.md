# indexer-cursor

`indexing.subscribe` resumes from the last acked change after a restart, and
only the identity that registered an indexer consumes or deletes it.

- **roster** node1 · **after** `bootstrap-user-software-key` · **restart**
  node1 · `destroys`
- **driver** `script.py` — apps I and J get an unconstrained
  `mod.auth.serve_objects_action` grant, which covers the indexer role. I
  registers an indexer, and the User enables `local` with
  `indexing.enable_repo`. I drains the backlog, the User stores a, b and c,
  and I acks a, receives b, and closes without acking b. The User stores d
  and deletes c. J drains an indexer of its own, then subscribes with I's
  nonce and acks whatever it is sent.
- **oracle** `verify.py` — after the restart, subscribes as I with the token
  the driver recorded, then unregisters as J and as I.
- **helper** `lib/indexing.py` — the indexing ops as raw JSON queries, since
  astral-py binds no `indexing` module.

## What it covers

No other test drives any of the four indexing ops. The subscription delivers
each change at least once, in version order, and commits the persisted cursor
only after an ack that names the delivered repository and version
(`mod/indexing/src/op_subscribe.go`). A change sent and not acked is sent
again on the next subscription, including one after a restart.

The owner check answers a non-owner `index not found`, the same answer as an
unknown nonce, on both `indexing.subscribe` and
`indexing.unregister_indexer`. A non-owner that reached the stream would
advance the owner's cursor with its own acks, so J acks every change it
receives: an admitted J moves I's cursor past b, and the oracle sees that
twice, in J's answer and in I's first message after the restart.

## Claims

1. Before the restart, a was acked and b, the next version, was not.
2. J's subscribe with I's nonce answers `index not found` alone. J's own
   indexer delivers d, so J holds the permit and the refusal is the owner
   check.
3. After the restart, I's first message is b at its earlier version.
4. Index c, index d and the unindex of c follow in that order, at
   consecutive versions.
5. J's unregister with I's nonce answers `index not found`. I's unregister
   answers an ack, and a later subscribe answers `index not found`.

## The unindex of c comes from the restart

The index follows a repository through `Repository.Scan` with follow, and a
scan reports added objects only (`mod/indexing/src/repos.go`, `syncRepo`). A
live delete therefore writes no unindex change. The node writes one when the
next resync of `local` compares a fresh snapshot with the index, which the
restart triggers. The driver's probe indexer records whether the live delete
produced an unindex (`probe` fact); the oracle reports it and asserts it
nowhere.

## Draining the backlog

Enabling `local` indexes what the repository already holds, in snapshot
order, in a background goroutine. The driver acks that backlog until the
stream is silent for three seconds, so a, b, c and d are the newest changes
and their store order is their version order.
