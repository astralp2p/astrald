# Objects

## Core Types

### Object

* Immutable typed payload declaring a type string via `ObjectType()` — see
  [object](../../system/core-definitions/object.md) and
  [object-type](../../system/core-definitions/object-type.md).
* Serialization (`WriteTo`/`ReadFrom`, `astral.Objectify`) is provided by
  astral-go — see astral-go `.ai/knowledge/concepts/wire.md`.
* Unknown types decode as opaque `*astral.Blob` only when the astral magic stamp is absent; other decode failures propagate (`mod/objects/src/module.go` `Load`).

### ObjectID

* Content address over the canonical encoding of an object.
* Wire and string encoding: see [object_id.sha256](../../system/primitive-types/object_id.sha256.md).
* Nodes compute ObjectIDs locally during `Commit`.

### Repository

* Stores raw bytes by ObjectID.
* `Create` returns a `Writer`; every writer must end in exactly one `Commit` or `Discard`. The `Writer` type is defined in astral-go `api/objects` (see astral-go `.ai/knowledge/api/objects.md`); `Repository.Create` returns it.
* `Scan(ctx, follow)` emits ObjectIDs; in follow mode it must emit exactly one nil between snapshot and live updates.
* `Read(ctx, id, offset, limit)` returns a `Reader`; `Delete`, `Contains`, and `Free` complete the surface.
* `RepoGroup` is a `Repository` plus `Add`/`Remove`/`List`; group reads run sequentially or concurrently, group scans collapse member snapshot boundaries into one.
* Optional `AfterRemovedCallback` is invoked when a repo is removed from the registry.

### Receiver

* Side of the receive path. Modules implementing `Receiver` see every locally received or pushed object via a `Drop`.

### Drop

* Carries the sender identity, the object, and the save target (`WriteDefault`).
* `Accept(save bool)` acknowledges the object; `save=true` stores it through the module's `Store` at most once even across multiple accepting receivers.
* Not calling `Accept` silently passes; other receivers still run.

## Repository Groups

| Group       | Purpose                                                  |
|-------------|----------------------------------------------------------|
| `local`     | primary on-disk; default write target (`WriteDefault`)   |
| `memory`    | in-memory caches (seeded with `mem0` and `system`)       |
| `removable` | portable/external media                                  |
| `device`    | combined `memory` + `local` + `removable`                |
| `virtual`   | computed sources — archives, encryption wrappers         |
| `network`   | remote peers (requires `ZoneNetwork`)                    |
| `system`    | internal node data (in-memory by default)                |
| `main`      | `device` + `virtual` + `network`; default read target    |

## Tracking and Purge

* `objects__objects` rows pair each known ObjectID with a type, creation time, monotonic `height`, and `read_at`.
* Rows are seeded by `Store`, `Load`, `Probe`, `GetType`, and `OpCreate` (via `Probe`). Seeding is idempotent and best-effort.
* A read journal records every qualified read in memory and flushes batched `read_at` UPDATEs to the DB on purge entry and shutdown; first reads do not seed.
* Purge walks tracked IDs by `(read_at, height)` keyset, oldest-first, asking every `Holder` whether each ID is still in use; unheld IDs are deleted through the requested repository.

## Discovery Extension Points

Modules implement these interfaces; the objects module discovers implementations
through type assertions in `LoadDependencies`. External (caller-hosted)
discoverers register at runtime through `objects.register_*` ops and are
deduplicated by `SourceIdentity`. The `Describer`, `Searcher`,
`SearchPreprocessor`, and `Finder` interface definitions now live in astral-go
`api/objects` (see astral-go `.ai/knowledge/api/objects.md`); `Receiver`,
`Holder`, and `SourceIdentifier` stay in astrald `mod/objects`, and the
node-side discovery/dispatch above stays in `mod/objects`.

| Interface | Trigger |
|---|---|
| `Receiver` | locally received or pushed object; accept and optionally persist via `Drop.Accept` |
| `Describer` | metadata request for an ObjectID |
| `Searcher` | text/tag search over module-owned indexes |
| `SearchPreprocessor` | mutates `objects.Search` before searchers run |
| `Finder` | provider lookup by ObjectID |
| `Holder` | purge-time protection; held objects are skipped by `objects.purge`, not by `objects.delete` |
| `SourceIdentifier` | marks an extension as proxying for an external identity; enables dedup |

`Holder` is a purge-time protection hook. `objects.delete` is a direct repository
command and does not consult holders. `Holder.HoldObject` returning an error
should fail closed (return `true`) — losing the cache row is preferable to
deleting referenced data.

| Holder provider | Protected objects |
|---|---|
| `apphost` | rows in `apphost__object_holds` (explicit app-owned holds) |
| `auth` | active indexed signed-contract objects used for authorization |
| `crypto` | indexed private-key objects (and their corresponding public-key objects) used for signing |
| `user` | active user asset rows |
