# Tree

`mod/tree` is a hierarchical key-value store.

* Every node can hold an `astral.Object`.
* Every node can have named children.
* Paths use the `/segment/segment` convention.
* `Mount` can replace any subtree with a custom `Node` implementation.
* `MountRemote` can mount a remote node from another Astral identity.

The tree is network-addressable. An operator on another machine can write
values into a peer's tree over an encrypted Astral connection. Treat the tree as
the node's shared runtime state layer, not only as local config.

## Live Config Binding

Modules bind settings to tree paths with `tree.Value[T]` via `mod/tree`
`Bind`/`BindPath`. The `tree.Value[T]` cell itself — a typed, observable cell
with `Get`/`Follow` read APIs — now lives in astral-go `api/tree` (see astral-go
.ai/knowledge/concepts/tree.md); `mod/tree` only consumes it. On the default
DB-backed tree node a bound value survives restarts through the DB.

Use `Follow(ctx)` when module behavior must react continuously. For example,
`mod/tcp` toggles its listener goroutine on or off as `settings.Listen`
changes.

### Invariants

* A module cannot refuse a new value from `Follow(ctx)`.
* A module reacts to whatever value arrives.

### Paths

* Use `/mod/<name>/config` for persistent config.
* Use `/mod/<name>/settings` for runtime-togglable state.
* `Bind()` wires a struct's `tree.Value` fields automatically, using snake-cased
  field names as path segments, overridable per field with the `tree` struct tag.
* `BindPath()` queries the node for a path (optionally creating it) and then
  calls `Bind`.
