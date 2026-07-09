# mod/indexing

Tracks object membership in named object repositories as an append-only, versioned changelog, and delivers each change exactly once to subscribed indexers. Owns the per-repo change log, indexer registrations with per-repo cursors, and the long-lived subscribe stream that drives external consumers.

## Dependencies

| Module | Why |
|---|---|
| `objects` | `Objects.GetRepository(name)` and `Repository.Scan(ctx, follow=true)` provide the snapshot-then-live object stream that the sync loop consumes |
| `tree` | enabled repos persist as children of `/mod/indexing/repos`; indexer registrations and per-repo cursors persist under `/mod/indexing/indexers/<name>` |
| `core/assets` | `LoadYAML` reads config and `Database()` backs the `indexing__repo_entries` table |
| `gorm` | `DB` migrates and serves the per-repo append-only changelog used to compute next pending changes |
| astral-go `lib/routing` | `OpRouter.AddStructPrefix` exposes `OpRegisterIndexer`, `OpSubscribe`, `OpRemoveIndex`, `OpEnableRepo` |
| astral-go `astral/channel` | subscribe streams `IndexMsg`/`UnindexMsg` and expects `ChangeAckMsg` or temporary failure on the same channel |
| astral-go `sig` | `sig.Map` tracks per-repo sync cancel funcs; `sig.NewRetry` paces retries on unacked changes |

## Flows

- Module setup: `Load` reads config -> registers `Op*` handlers -> opens DB -> migrates `indexing__repo_entries`; `LoadDependencies` resolves deps and creates the `/mod/indexing/repos` and `/mod/indexing/indexers` tree nodes.
- Resume on start: `Run` lists children of `repos` and calls `startRepoSync` for each, then blocks until context cancellation.
- Enable repo: `OpEnableRepo` -> if `Disable`, `DisableRepo` cancels the sync goroutine and removes the tree child; otherwise `EnableRepo` creates the tree child and `startRepoSync` launches the goroutine.
- Repo sync: `syncRepo` calls `Repository.Scan(follow=true)` -> drains until the snapshot-boundary nil -> diffs the snapshot against `latestExistingObjectIDs` -> writes missing/excess changes through `addToRepo`/`removeFromRepo` -> then follows live IDs from `scan`, appending new versions; each write calls `broadcastChange` to wake subscribers.
- Register indexer: `OpRegisterIndexer` -> `RegisterIndexer` returns the existing nonce by name or creates `indexers/<name>` and stores a fresh `astral.Nonce`; concurrent create races resolve to the winner's nonce.
- Subscribe: `OpSubscribe` -> `findIndexerByNonce` -> loop: `pickNextChange` scans enabled repos for the first entry with `version > cursor` -> if none, wait on `changeSignal` or ctx -> send `IndexMsg`/`UnindexMsg` -> expect `ChangeAckMsg` matching `(repo, version)` -> `UpdateIndexerState` advances cursor (must equal `current+1`) -> on `ErrIndexingTemporarilyFailed` reuse pending change after `retry` backoff.
- Remove indexer: `OpRemoveIndex` -> `RemoveIndexer` resolves by nonce -> `deleteIndexerTree` deletes per-repo cursor sub-nodes depth-first then the indexer node.

## Source

- `mod/indexing/module.go`, `errors.go`, `indexer.go` - public `Module` interface, `ModuleName`/`DBPrefix`, node-side sentinels, and the `Indexer` interface. Wire messages (`IndexMsg`/`UnindexMsg`/`ChangeAckMsg`), op-name constants (`Method*`), and ack-related sentinels live in astral-go `api/indexing` (see astral-go .ai/knowledge/api/indexing.md).
- `mod/indexing/src/loader.go`, `module.go`, `deps.go`, `config.go` - construction, tree-node wiring, dependency injection, lifecycle.
- `mod/indexing/src/repos.go` - enable/disable, sync goroutine, snapshot-boundary handling, change broadcast.
- `mod/indexing/src/indexers.go` - indexer handles, per-repo cursors, change picker.
- `mod/indexing/src/db.go`, `db_repo_entry.go` - append-only changelog, latest-state queries, `nextChange`.
- `mod/indexing/src/op_register_indexer.go`, `op_subscribe.go`, `op_remove_index.go`, `op_enable_repo.go` - query handlers.
- The typed client wrapper and the `Subscription` handle (`Next`/`Ack`/`Fail`) live in astral-go `api/indexing/client` (see astral-go .ai/knowledge/api/indexing.md).

## Surface

| What | Why it matters |
|---|---|
| `indexing.register_indexer`, `indexing.remove_index` | stable nonce lifecycle for external indexers |
| `indexing.subscribe` | one-change-at-a-time stream with explicit ack and temporary-failure retry |
| `indexing.IndexMsg`, `indexing.UnindexMsg`, `indexing.ChangeAckMsg` | wire format for change delivery and acknowledgement; the Go types live in astral-go `api/indexing` and their encoding is specified under [system/protocols/indexing/types](../../system/protocols/indexing/types) |
| `/mod/indexing/repos`, `/mod/indexing/indexers/<name>` | persisted enabled-repo set and per-indexer cursor tree |
| `indexing__repo_entries` | append-only changelog with monotonic `Version` per repo |

## Invariants

- `Repository.Scan(follow=true)` must emit exactly one nil after the snapshot and before live updates; missing or duplicate boundary aborts sync with an error.
- `Version` is monotonic per repo; `addToRepo` rejects when the latest entry already exists, `removeFromRepo` rejects when it does not.
- Cursor advances are strictly `+1`; `UpdateIndexerState` returns `ErrInvalidIndexHeight` otherwise.
- A subscriber must reply with `ChangeAckMsg` matching `(repo, version)` of the last delivered change; mismatches send `ErrAckMismatch` and end the stream.
- `ErrIndexingTemporarilyFailed` from the subscriber keeps the same pending change for retry with capped backoff.
- One sync goroutine per repo: `EnableRepo` returns `ErrRepoAlreadySyncing` if already running.
