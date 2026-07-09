# mod/indexing

Tracks object membership in named object repositories as an append-only, versioned changelog, and delivers each change exactly once to subscribed indexers. Owns the per-repo change log, indexer registrations with per-repo cursors, and the long-lived subscribe stream that drives external consumers.

## Dependencies

| Module | Why |
|---|---|
| `objects` | `Repository.Scan(follow=true)` provides the snapshot-then-live object stream the sync loop consumes |
| `tree` | enabled repos persist under `/mod/indexing/repos`; indexer registrations and per-repo cursors persist under `/mod/indexing/indexers/<name>` |
| `core/assets` | reads config and backs the `indexing__repo_entries` table |
| `gorm` | migrates and serves the append-only changelog used to compute the next pending change |
| astral-go `lib/routing` | exposes the `OpRegisterIndexer`/`OpSubscribe`/`OpRemoveIndex`/`OpEnableRepo` handlers |
| astral-go `astral/channel` | the subscribe stream carries change messages and expects an ack or temporary failure on the same channel |
| astral-go `sig` | `sig.Map` tracks per-repo sync cancel funcs; `sig.NewRetry` paces retries on unacked changes |

## Flows

* Resume on start: `Run` lists children of `repos` and calls `startRepoSync` for each, then blocks until context cancellation.
* Repo sync: `syncRepo` calls `Repository.Scan(follow=true)` -> drains until the snapshot-boundary nil -> diffs the snapshot against `latestExistingObjectIDs` -> writes missing/excess changes via `addToRepo`/`removeFromRepo` -> then follows live IDs, appending new versions; each write wakes subscribers.
* Subscribe loop: `pickNextChange` finds the first entry with `version > cursor` across enabled repos -> waits on `changeSignal` when none -> delivers the change -> requires an ack matching `(repo, version)` -> `UpdateIndexerState` advances the cursor by exactly one -> on temporary failure reuses the same pending change after backoff.

## Invariants

* `Repository.Scan(follow=true)` must emit exactly one nil after the snapshot and before live updates; a missing or duplicate boundary aborts sync with an error. (Shared snapshot-boundary contract with `fs.md` and `objects`.)
* `Version` is monotonic per repo; `addToRepo` rejects when the latest entry already exists, `removeFromRepo` rejects when it does not.
* Cursor advances are strictly `+1`; `UpdateIndexerState` returns `ErrInvalidIndexHeight` otherwise.
* A subscriber must reply with an ack matching `(repo, version)` of the last delivered change; a mismatch sends `ErrAckMismatch` and ends the stream.
* `ErrIndexingTemporarilyFailed` from the subscriber keeps the same pending change for capped-backoff retry.
* Delivery is exactly-once.
* One sync goroutine per repo: `EnableRepo` returns `ErrRepoAlreadySyncing` if already running.
* `RegisterIndexer` returns the existing nonce by name; a concurrent create resolves to the winner's nonce.
