# app-index-provider

An indexer app follows a repository on node1. The User stores a note in that
repository, and node1 tells the app about it.

- **roster** node1 · **after** `bootstrap-user-software-key`
- **driver** `script.py` — runs `driver/` (Go, this repository's astral-go) in
  `drive` mode:
  1. The User creates a test-unique memory repository with `objects.new_mem`
     and turns indexing on for it with `indexing.enable_repo`.
  2. The User registers the indexer app with `apphost.register`, asking for
     `mod.auth.serve_objects_action` limited to the `indexer` role.
  3. The app registers an indexer with `indexing.register_indexer` and opens
     `indexing.subscribe` with the nonce node1 returns.
  4. The User stores a `String16` note in the repository with `objects.store`.
     The app's next change is `indexing.index` of the note in that repository,
     and the app acks it.
  5. The User revokes the app's grant with `apphost.revoke`. The app's next
     `indexing.subscribe` answers `not permitted`.
- **oracle** `verify.py` — every step held, plus the same story with a fresh
  app and a fresh repository (`driver verify`).

## How node1 tells the app

An indexer app serves no op: it holds the subscription open, and node1 writes
each change to it. `indexing.enable_repo` starts following the repository's
`Scan` (`mod/indexing/src/repos.go`), each object the scan reports becomes a
change, and `indexing.subscribe` sends the indexer every change past its cursor
(`mod/indexing/src/op_subscribe.go`). `indexing.subscribe` checks the indexer
grant when a subscription starts, so the refusal comes on the subscription
after the revoke.

`indexer-cursor` covers the cursor across a restart and the owner check, in a
world of its own. This test covers the plain flow, through the astral-go
`api/indexing` client, in the shared world.

The driver leaves the shared world as it found it: the app unregisters its
indexer, which needs no permit from the owner, and the User turns indexing off
for the repository and removes it. A cleanup step that fails fails the driver,
with the prefix `cleanup:`.
