# mod/objects

Hosts the node's content-addressed object layer behind a uniform query and repository surface. Owns default repository groups, an object tracking index seeded by every "object entered the node" path, an in-memory reads journal that backs purge ordering, and extension registries for describers, searchers, search preprocessors, finders, holders, and receivers.

## Dependencies

| Module | Why |
|---|---|
| `auth` | `OpRead` authorizes `ReadObjectAction` before serving object bytes |
| `dir` | `fetchARL` parses `astral://` ARLs with `arl.Parse` and resolves them through the directory |
| `nodes` (opt) | injected for module integration; outbound network object calls go through astral-go `api/objects/client` via `lib/astrald` `astrald.Default()` |
| `core/assets` | loads `objects.yaml`, supplies the gorm DB that backs the `objects__objects` tracking table, and creates the default in-memory `mem0` and `system` repositories |
| `core.Node` | `LoadDependencies` walks `Modules().Loaded()` to auto-register object extension interfaces |

## Flows

- Store object: `Store` -> repository `Create` -> canonical encode via `astral.CanonicalTypeEncoder` -> `Commit` -> `trackObject` seeds `dbObject` -> return `*ObjectID`.
- Load object: repository `Read` -> read bytes into memory -> `objectsReadsJournal.Mark` -> `astral.Decode` with `astral.Canonical()` -> on success `trackObject` and return object -> "invalid magic bytes" returns `*astral.Blob` (without seeding); other decode errors propagate.
- Create stream: `OpCreate` acks the query -> drains incoming `*astral.Blob` chunks into a repository writer -> on `objects.CommitMsg` commits, calls `Probe` to seed `dbObject` with the parsed type, returns the `*ObjectID`; the deferred `Discard` protects unfinished writers.
- Read stream: `OpRead` resolves repo and range -> `Auth.Authorize(ReadObjectAction)` -> repository `Read` -> `objectsReadsJournal.Mark` -> streams raw bytes to `q.AcceptRaw()`.
- Repository group read: sequential groups walk members in priority order; concurrent groups race member reads and return the first successful reader, cancelling the rest.
- Repository group scan: each member scans in its own goroutine -> in `follow` mode member snapshot terminators are collapsed into one nil written to the group channel; in non-follow mode member nils are dropped and the group closes when every member closes.
- Search: preprocessors mutate `objects.Search` -> local searchers run concurrently -> in `ZoneNetwork`, each `search.Sources` identity is queried through `objectscli.New(...).Search` -> `OpSearch` deduplicates by `ObjectID`, optionally filters by `repo.Contains`, terminates with `EOS`.
- Describe: every registered `Describer` runs concurrently and forwards descriptors; `OpDescribe` applies `only`/`except` type filters and terminates with `EOS`.
- Find: every registered `Finder` runs concurrently; `OpFind` deduplicates providers by identity string and terminates with `EOS`.
- Receive local object: `Receive` defaults zero source to node identity, builds a `Drop` whose save target is `WriteDefault`, calls every receiver; success = any receiver called `Accept`. `Drop.Accept(true)` is guarded by a mutex and runs `Store` at most once.
- Push object: local target short-circuits through `Receive`; remote target dials `objects.push` via `objectscli.New(target, nil).Push` and expects a boolean per object.
- Purge repository: `OpPurge` opens the channel and calls `purgeRepository` -> flush `objectsReadsJournal` -> keyset-paginate `dbObject` by `(read_at, height)` 256 at a time -> for each id skip if any registered `Holder.HoldObject` returns true, otherwise `repo.Delete`. `ErrNotFound` deletes drop the stale tracking row via `DeleteObjectCacheByID`; `errors.ErrUnsupported` is skipped; successful deletes are streamed to the caller; the stream ends with `EOS`.
- Fetch object: `fetch` dispatches on scheme. `http`/`https` use `http.Get` and write into `WriteDefault`; `astral://` calls `arl.Parse(..., mod.Dir)`, routes the in-flight query, and writes the response into `WriteDefault`.
- Reads journal lifecycle: `OpRead` and `Module.Load` call `objectsReadsJournal.Mark` on the hot path (in-memory only). `purgeRepository` and `Module.Run` shutdown call `Flush`, which atomically drains the pending map and UPDATEs `read_at` for already-tracked rows; first reads do not seed.
- Extension discovery: `LoadDependencies` injects `Deps`, then iterates `cnode.Modules().Loaded()` and registers any module that satisfies `Describer`, `Searcher`, `SearchPreprocessor`, `Finder`, `Holder`, or `Receiver`.
- External discoverer registration: `OpRegisterDescriber`/`OpRegisterFinder`/`OpRegisterSearcher` reject `OriginNetwork`, validate the caller identity (non-zero, not self), and add an `ExternalDescriber`/`ExternalFinder`/`ExternalSearcher` that proxies via `objectscli.New(callerID, astrald.Default())` with a 15s timeout. `Add*` deduplicates by `SourceIdentity`.
- Repository removal: `RemoveRepository` deletes the repo from the registry, removes it from every group, and calls `AfterRemoved(name)` when the repo implements `AfterRemovedCallback`.

## Source

- `mod/objects/module.go`, `mod/objects/repository.go`, `mod/objects/reader.go`, `mod/objects/read_seeker.go`, `mod/objects/errors.go` - public module, repository, and reader contracts and sentinels.
- `mod/objects/source_identifier.go` - source-identity helper. The extension/wire types (`Writer`, `Descriptor`, `Search`, `SearchQuery`, `SearchResult`, `Find`, `Probe`), the `ReadObjectAction`/`CreateObjectAction` auth actions, and the typed clients live in astral-go `api/objects/` (see astral-go .ai/knowledge/api/objects.md).
- `mod/objects/src/loader.go`, `mod/objects/src/deps.go`, `mod/objects/src/module.go`, `mod/objects/src/config.go` - default repo layout, dependency injection with extension auto-registration, lifecycle.
- `mod/objects/src/db.go`, `mod/objects/src/db_object.go`, `mod/objects/src/objects_reads_journal.go` - `dbObject` tracking row, keyset cursor for purge order, and the in-memory reads journal.
- `mod/objects/src/repositories.go`, `mod/objects/src/repo_group.go` - repository registry, group operations (read, create, contains, delete, scan, free).
- `mod/objects/src/drop.go`, `mod/objects/src/receive.go`, `mod/objects/src/push.go`, `mod/objects/src/fetch.go`, `mod/objects/src/network_reader.go` - object receive, save-on-accept, push, fetch, and routed network reads.
- `mod/objects/src/describe.go`, `mod/objects/src/search.go`, `mod/objects/src/find.go`, `mod/objects/src/holding.go` - local extension dispatch and holder aggregation.
- `mod/objects/src/external_describer.go`, `mod/objects/src/external_finder.go`, `mod/objects/src/external_searcher.go` - node-side adapters (over astral-go `api/objects/client`) used by the external-registration ops.
- `mod/objects/src/purge.go`, `mod/objects/src/op_purge.go` - read-order purge driver and op handler.
- `mod/objects/src/op_*.go` - query handlers for object storage, reads, scans, search, describe, find, repo management, probes, type lookup, push, echo, and external registration.
- The typed remote clients used by push, search, create, read, scan, purge, repository management, and external registration calls live in astral-go `api/objects/client/` (see astral-go .ai/knowledge/api/objects.md).
- `mod/objects/mem/`, `mod/objects/fs/`, `mod/objects/views/` - in-memory repo, filesystem adapter, and presentation helpers.

## Surface

| What | Why it matters |
|---|---|
| `objects.new`, `objects.load`, `objects.store`, `objects.create`, `objects.read`, `objects.delete`, `objects.purge`, `objects.contains` | core object storage and byte streaming operations |
| `objects.scan`, `objects.search`, `objects.describe`, `objects.find`, `objects.probe`, `objects.get_type` | discovery, metadata, type, and inspection operations |
| `objects.push`, `objects.echo` | object delivery and connectivity helpers |
| `objects.repositories`, `objects.remove_repository`, `objects.new_mem` | repository management |
| `objects.register_describer`, `objects.register_finder`, `objects.register_searcher` | external (caller-hosted) discovery registration |
| `objects.register_blueprint`, `objects.blueprints`, `objects.get_blueprint` | runtime type registration (struct-kind and alias-kind Blueprints through one op), type-name listing, and single-type Blueprint lookup (`OpGetBlueprint`); backs the astral-go `apps.WithBlueprintSync` flow (see astral-go .ai/knowledge/concepts/blueprints.md) |
| `Receiver`, `Describer`, `Searcher`, `SearchPreprocessor`, `Finder`, `Holder` | extension points auto-discovered from loaded modules |
| `main`, `device`, `memory`, `local`, `removable`, `virtual`, `network`, `system` | default repository groups and built-in repositories |
| `objects__objects` | tracking row (height, id, type, created_at, read_at) used by purge order and lazy type lookups |

## Invariants

- Every writer must end in exactly one `Commit` or `Discard`; `OpCreate` defers `Discard` as a leak guard.
- `objects.delete`, `objects.contains`, and `objects.scan` require an explicit repository (no default).
- `objects.purge` is the cleanup path that honors `Holder`; `objects.delete` is a direct repository command and skips holders.
- `Holder` registration is automatic for loaded modules that implement `objects.Holder`; disabling a provider module removes that provider's purge protection. `HoldObject(*astral.ObjectID) bool` is the whole contract; failure handling inside a holder is the provider's concern.
- `objects.repositories` excludes the network zone.
- `Load` returns `*astral.Blob` only for invalid astral magic bytes; other decode failures propagate.
- `dbObject` rows are seeded by `Store`, `Load` (on successful decode), `Probe`, `GetType`, and `OpCreate` (via `Probe`); seeding is idempotent (`INSERT OR IGNORE`) and seeding failure is logged, never propagated.
- `objectsReadsJournal.Mark` is in-memory only; persistence happens on `Flush` (purge entry and `Module.Run` shutdown) and is UPDATE-only — first reads do not seed.
- Purge iterates `dbObject` by `(read_at, height)` keyset; stale rows (delete returns `ErrNotFound`) are pruned from the cache; `errors.ErrUnsupported` deletes are silently skipped.
- `Drop.Accept(true)` saves at most once even if multiple receivers accept with save.
- Repository scans with `follow=true` must emit exactly one nil between snapshot and live updates; `OpScan` forwards each nil as `astral.EOS`, then sends a final `EOS` when the scan channel closes.
- `AddRepository` rejects duplicate names; `RemoveRepository` removes the repo from all groups and calls `AfterRemoved(name)` when implemented.
- `AddDescriber`/`AddFinder`/`AddSearcher` deduplicate registrations carrying a `SourceIdentifier` by source identity (external proxies cannot register twice).
- `OpRegisterDescriber`/`OpRegisterFinder`/`OpRegisterSearcher` reject `OriginNetwork` and the node's own identity.
- `ReadDefault` is `main`; `WriteDefault` is `local`.
- `OpBlueprints` (`objects.blueprints`) streams names from `DefaultBlueprints().OrderedBlueprints()` as `String8`: compile-time prototypes first (alpha-sorted), then aliases (alpha-sorted, leaves), then runtime Blueprints topo-sorted by reference; terminated with `EOS`. Aliases precede runtime Blueprints so a Blueprint's RefSpec to an alias resolves when peers replay in order.
- `OpRegisterBlueprint` (`objects.register_blueprint`) runs in batch mode: it reads `*astral.Blueprint` values (struct kind or alias kind) until `EOS` or EOF, registers each via `Module.Register` → `Blueprints.Register`, and answers each input with the `ObjectID` or a wire-error before a final `EOS`. Name collision returns `astral.ErrAlreadyRegistered` as a wire-error; the client-side wire-string-to-sentinel translation lives in astral-go `api/objects/client` (see astral-go .ai/knowledge/api/objects.md).
- Blueprint/alias registry internals (the shared `Blueprints.entries` registry map, `RegisterBlueprint` `ErrAlreadyRegistered` semantics, `PrimitiveAlias`/`UnderlyingPrimitive`, `BlueprintOf` derivation, `New` returning typed Go values vs a `*RuntimeObject`) and the `apps.WithBlueprintSync`/`SyncBlueprints` diff-and-push flow live in astral-go — see astral-go .ai/knowledge/concepts/blueprints.md.
- todo(security): neither `objects.blueprints` nor `objects.register_blueprint` is gated by caller identity; any peer can squat a type name or enumerate the registry.
