# mod/fs

Exposes local directories as `objects.Repository` instances so the node can store, read, describe, and search content-addressed objects from disk. Owns writable filesystem repositories, read-only watched repositories, the local-file index, and fsnotify-driven reindexing for paths under watched roots.

## Dependencies

| Module | Why |
|---|---|
| `objects` | registers repositories with `AddRepository`, adds them to `RepoLocal`, and implements the `Repository`, `AfterRemovedCallback`, `Describer`, and `Searcher` contracts |
| `core/assets` | reads fs config, backs `fs__local_files`, and provides the default `<data root>/data` repo |
| `auth`, `dir`, `shell` (opt) | injected in `Deps`; current source does not call them |
| `fsnotify` | drives `Watcher` events for writes, renames, creates, chmods, and removes |
| `golang.org/x/time/rate` | bounds stat, hash, and enqueue work in `Indexer` |
| `lib/paths` | walks watched roots, collapses overlapping roots, and checks path coverage |

## Flows

* Writable object commit: `Repository.Create` checks requested allocation against free space -> `NewWriter` writes to a temp file -> `Commit` resolves the object ID -> atomic finalized compare-and-swap -> rename the temp file to the object ID.
* Indexer worker: pop path -> soft-delete uncovered paths -> stat covered paths -> validate unchanged rows -> hash changed files -> upsert the index row -> publish `IndexEvent` when subscribers exist.

## Invariants

* Writable repository files are named exactly `ObjectID.String()`; non-regular and unparseable filenames are skipped during scan.
* `Repository.Read`, `DescribeObject`, and `SearchObject` require `ZoneDevice`; `WatchRepository.Read` does not check the zone.
* `WatchRepository` is read-only: `Create` and `Delete` return `errors.ErrUnsupported`, and `Free` returns `0`.
* Watch roots are cleaned absolute directories that must exist at construction time.
* Active index rows require `updated_at != 0` and `deleted_at IS NULL`; `updated_at = 0` means the path needs recheck.
* `Writer` finalization is single-use; a repeated `Commit` or `Discard` returns `writer closed`.
* Follow scans emit exactly one `nil` object ID as the snapshot/follow boundary. (Shared boundary contract consumed by `indexing`; see `indexing.md`.)
* `IndexEvent` fan-out is active only while `subscriberCount > 0`.
* Removing a watch root soft-deletes only paths not covered by another remaining root.
* `Repository.Create` rejects with `objects.ErrNoSpaceLeft` when `opts.Alloc` exceeds reported free space.
* `SearchObject` requires the `path` tag and matches indexed paths case-insensitively.
