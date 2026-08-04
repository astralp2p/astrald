# mod/dir

Maps human-readable names to identities and back, using a persistent alias table before a pluggable resolver chain. Owns named identity filters that can gate query targets, and publishes the local alias into nearby status when the node is visible.

## Dependencies

| Module | Why |
| --- | --- |
| `nearby` | `ComposeStatus` checks `Nearby.Mode()` and attaches `dir.Alias` when visible; injected via `core.Inject`, which fails when the module is missing |
| `astral.Node` | local identity backs `localnode` resolution, default-alias setup, filter bypass for local targets, and display-alias lookup |
| `core/assets` | `Database()` backs `dir__aliases`; `LoadYAML` loads the empty dir config |
| `gorm` | migrates and queries alias rows |

## Invariants

* The alias table precedes the resolver chain in both `ResolveIdentity` and `DisplayName`.
* `DisplayName` is never empty: zero identity returns `"<anyone>"`; otherwise fingerprint fallback.
* `SetAlias(id, "")` deletes the row; aliases are unique.
* Local-target queries bypass the filter gate in `PreprocessQuery`.
* Empty `q.Extra["filters"]` falls back to `DefaultFilters()`; an empty default allows the target.
* Filter-registration contract: other modules register named filters via `SetFilter` (`nodes` registers `linked`; `user` registers `localswarm` and `localuser`). `dir` installs `all` and `localnode` and sets the default to `all`.
* Resolver-registration contract: other modules register a `dir.Resolver` via `AddResolver` (`dir` registers `DNS`; `nearby` resolves dot-prefixed aliases; `user` resolves `localuser`). A resolver declines a name by returning an error, and `ResolveIdentity` takes the first nil error as a hit, so a resolver must never return a nil identity without an error. Resolver errors are discarded: the caller sees `unknown identity: <name>`.
* Reserved-name precedence differs by source: `localnode` is checked before the alias table; a resolver-supplied name such as `localuser` is consulted after it, so an alias can shadow it.
* `ComposeStatus` attaches `dir.Alias` only in `ModeVisible`.
