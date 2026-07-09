# Objects

Node-side contract for the content-addressed object layer. The `Object`,
`ObjectID`, and `Writer` types live in astral-go `api/objects`; protocol meaning
is in the spec. This note holds the daemon-side invariants that cross modules.

## Writer

* Every `Writer` from `Repository.Create` must end in exactly one `Commit` or
  `Discard`.
* Nodes compute the `ObjectID` locally during `Commit`.

## Scan

* `Repository.Scan(ctx, follow)` in follow mode emits exactly one nil between
  the snapshot and live updates.
* `RepoGroup` scans collapse member snapshot boundaries into one nil.

## Receive

* A `Drop` carries the sender identity, the object, and the save target
  (`WriteDefault`).
* `Drop.Accept(save)` acknowledges; `save=true` stores through the module's
  `Store` at most once even across multiple accepting receivers.
* Not calling `Accept` passes silently; other receivers still run.

## Decode

* Unknown types decode as opaque `*astral.Blob` only when the astral magic stamp
  is absent; other decode failures propagate.

## Repository Groups

Canonical home for the group-to-zone mapping; `concepts/zone.md` points here.

| Group       | Purpose                                                |
|-------------|--------------------------------------------------------|
| `local`     | primary on-disk; default write target (`WriteDefault`) |
| `memory`    | in-memory caches (seeded with `mem0` and `system`)     |
| `removable` | portable/external media                                |
| `device`    | `memory` + `local` + `removable`                       |
| `virtual`   | computed sources — archives, encryption wrappers       |
| `network`   | remote peers (requires `ZoneNetwork`)                  |
| `system`    | internal node data (in-memory by default)              |
| `main`      | `device` + `virtual` + `network`; default read target  |

## Discovery Interfaces

`Receiver`, `Holder`, and `SourceIdentifier` stay in astrald `mod/objects`;
`Describer`, `Searcher`, `SearchPreprocessor`, and `Finder` moved to astral-go
`api/objects`. Node-side discovery and dispatch stay in `mod/objects`.

* Modules implement these interfaces; `mod/objects` discovers implementations
  through type assertions in `LoadDependencies`.
* External (caller-hosted) discoverers register at runtime through
  `objects.register_*` ops and are deduplicated by `SourceIdentity`.

| Interface | Trigger |
|---|---|
| `Receiver` | locally received or pushed object; accept and optionally persist via `Drop.Accept` |
| `Describer` | metadata request for an ObjectID |
| `Searcher` | text/tag search over module-owned indexes |
| `SearchPreprocessor` | mutates `objects.Search` before searchers run |
| `Finder` | provider lookup by ObjectID |
| `Holder` | purge-time protection; held objects skipped by `objects.purge`, not by `objects.delete` |
| `SourceIdentifier` | marks an extension as proxying for an external identity; enables dedup |

## Holder

* `Holder` is purge-only. `objects.delete` is a direct repository command and
  does not consult holders.
* `Holder.HoldObject` must fail closed: return `true` on error, since losing the
  cache row beats deleting referenced data.

| Holder provider | Protected objects |
|---|---|
| `apphost` | rows in `apphost__object_holds` (explicit app-owned holds) |
| `auth` | active indexed signed-contract objects used for authorization |
| `crypto` | indexed private-key objects (and their corresponding public-key objects) used for signing |
| `user` | active user asset rows |
