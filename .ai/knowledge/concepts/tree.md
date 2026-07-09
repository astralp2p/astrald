# Tree

`mod/tree` binds Go struct fields to a path-addressed namespace of astral objects and persists the bound values across restarts.

* `MountRemote` makes a tree network-addressable: an operator on another machine can write values into a peer's tree over an encrypted Astral connection. Treat a tree as the node's shared runtime state, not only local config.

## Binding

* `Bind()` wires a struct's `tree.Value` fields, mapping each field to its snake_case path segment, overridable per field with the `tree` struct tag.
* `BindPath()` queries the node for a path (optionally creating it) and then calls `Bind`.
* A bound value on the default DB-backed node survives restarts through the DB.

### Invariants

* A module cannot refuse a new value from `Follow(ctx)`; it must react to whatever value arrives.

### Paths

* `/mod/<name>/config` — persistent config.
* `/mod/<name>/settings` — runtime-togglable state.
