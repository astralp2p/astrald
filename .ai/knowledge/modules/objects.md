# mod/objects

Hosts the node's content-addressed object layer behind a uniform query and
repository surface. Owns the default repository groups, an object tracking index
seeded by every "object entered the node" path, an in-memory reads journal that
backs purge ordering, and the extension registries for describers, searchers,
search preprocessors, finders, holders, and receivers.

## Dependencies

| Module | Why |
|---|---|
| `auth` | `OpRead` authorizes reads before serving object bytes |
| `dir` | resolves `astral://` ARLs for `fetch` |
| `nodes` (opt) | backs outbound network object calls |
| `core/assets` | loads `objects.yaml`, supplies the gorm DB behind `objects__objects`, and creates the in-memory `mem0` and `system` repositories |
| `core.Node` | `LoadDependencies` walks `Modules().Loaded()` to auto-register extension interfaces |

## Flows

* Repository group read: sequential groups walk members in priority order;
  concurrent groups race member reads and return the first successful reader,
  cancelling the rest.
* Repository group scan (follow): each member scans in its own goroutine; member
  snapshot terminators collapse into one nil on the group channel. Non-follow:
  member nils are dropped and the group closes when every member closes.
* Purge: `purgeRepository` flushes the reads journal, then keyset-paginates
  `dbObject` by `(read_at, height)` 256 at a time, oldest-first. Each id is
  skipped if any registered `Holder.HoldObject` returns true, otherwise
  `repo.Delete`. `ErrNotFound` drops the stale tracking row; `ErrUnsupported` is
  skipped; deletes stream to the caller; the stream ends with `EOS`.
* Reads journal lifecycle: `OpRead` and `Module.Load` `Mark` on the hot path
  (in-memory only). `purgeRepository` and `Module.Run` shutdown `Flush`, which
  atomically drains the pending map and UPDATEs `read_at` for already-tracked
  rows; first reads do not seed.
* Extension discovery: `LoadDependencies` injects `Deps`, then iterates
  `Modules().Loaded()` and registers any module satisfying `Describer`,
  `Searcher`, `SearchPreprocessor`, `Finder`, `Holder`, or `Receiver`.
* External discoverer registration: `OpRegisterDescriber`/`OpRegisterFinder`/
  `OpRegisterSearcher` reject `OriginNetwork`, validate the caller identity
  (non-zero, not self), and add an external proxy deduplicated by
  `SourceIdentity`.
* Repository removal: `RemoveRepository` removes the repo from the registry and
  from every group, and calls `AfterRemoved(name)` when the repo implements
  `AfterRemovedCallback`.

## Invariants

* Every writer ends in exactly one `Commit` or `Discard`; `OpCreate` defers
  `Discard` as a leak guard.
* `objects.delete`, `objects.contains`, and `objects.scan` require an explicit
  repository (no default).
* `ReadDefault` is `main`; `WriteDefault` is `local`.
* `objects.purge` is the cleanup path that honors `Holder`; `objects.delete` is a
  direct repository command and skips holders.
* `Holder` registration is automatic for loaded modules that implement
  `objects.Holder`; disabling a provider module removes that provider's purge
  protection.
* Purge iterates `dbObject` by `(read_at, height)` keyset, oldest-first; stale
  rows (delete returns `ErrNotFound`) are pruned from the cache; `ErrUnsupported`
  deletes are silently skipped.
* `objectsReadsJournal.Mark` is in-memory only; persistence happens on `Flush`
  (purge entry and `Module.Run` shutdown) and is UPDATE-only — first reads do not
  seed.
* `dbObject` rows are seeded by `Store`, `Load` (on successful decode), `Probe`,
  `GetType`, and `OpCreate` (via `Probe`); seeding is idempotent
  (`INSERT OR IGNORE`) and seeding failure is logged, never propagated.
* `Load` returns `*astral.Blob` only for invalid astral magic bytes; other decode
  failures propagate.
* `Drop.Accept(true)` saves at most once even if multiple receivers accept with
  save.
* Repository scans with `follow=true` emit exactly one nil between snapshot and
  live updates; `OpScan` forwards each nil as `astral.EOS`, then sends a final
  `EOS` when the scan channel closes.
* Repository group reads run sequentially or concurrently per group kind
  (sequential = priority order; concurrent = first successful reader wins).
* `AddRepository` rejects duplicate names; `RemoveRepository` removes the repo
  from all groups and calls `AfterRemoved(name)` when implemented.
* `AddDescriber`/`AddFinder`/`AddSearcher` deduplicate registrations carrying a
  `SourceIdentifier` by source identity (external proxies cannot register twice).
* `OpRegisterDescriber`/`OpRegisterFinder`/`OpRegisterSearcher` reject
  `OriginNetwork` and the node's own identity.
* `OpBlueprints` (`objects.blueprints`) streams names from
  `DefaultBlueprints().OrderedBlueprints()` as `String8`: compile-time prototypes
  first (alpha-sorted), then aliases (alpha-sorted, leaves), then runtime
  Blueprints topo-sorted by reference; terminated with `EOS`. Aliases precede
  runtime Blueprints so a Blueprint's RefSpec to an alias resolves when peers
  replay in order.
* `OpRegisterBlueprint` (`objects.register_blueprint`) runs in batch mode: it
  reads `*astral.Blueprint` values (struct kind or alias kind) until `EOS` or
  EOF, registers each via `Module.Register`, and answers each input with the
  `ObjectID` or a wire-error before a final `EOS`. Name collision returns
  `astral.ErrAlreadyRegistered` as a wire-error.
* todo(security): neither `objects.blueprints` nor `objects.register_blueprint` is
  gated by caller identity; any peer can squat a type name or enumerate the
  registry.
