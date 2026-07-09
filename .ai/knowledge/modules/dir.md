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
* `ComposeStatus` attaches `dir.Alias` only in `ModeVisible`.
