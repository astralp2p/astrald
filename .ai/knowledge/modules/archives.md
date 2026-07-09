# mod/archives

Indexes ZIP archives stored as objects so their entries become individually addressable, searchable, and readable through the objects pipeline. Owns the persistent archive-and-entry index, the virtual-zone entry opener, the device-zone archive descriptor, and the per-entry read-authorization rule that delegates to the parent archive.

## Dependencies

| Module | Why |
|---|---|
| `objects` | describes `ArchiveDescriptor`, serves entries via `OpenObject`/`SearchObject`, and reads parent archive bytes through `ReadDefault` |
| `auth` | recurses into the parent archive's `ReadObjectAction` to decide entry access |
| `core` | module registration and dependency injection |

## Invariants

* `Index` is serialized by `mod.mu`; concurrent indexing of the same archive cannot race.
* `Describe` is device-zone only; `OpenObject` and `SearchObject` are virtual-zone only.
* `setCache` clears existing rows before rewriting, so re-indexing is idempotent and replaces stale entries.
* `AuthorizeObjectsRead` recursively delegates to the parent archive's `ReadObjectAction`; it returns false when the parent row is missing or when the entry ID equals the parent ID (self-reference guard).
* `SearchObject` requires the caller's `RequiredTags` to be a subset of `{path, archive}`.
* `contentReader` backward-seek reopens the zip entry and skips from the start.
