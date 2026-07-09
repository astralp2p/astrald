# mod/tree

Maintains a path-addressed namespace of astral objects backed by a database node table and live value subscriptions. Owns mount points for local or remote nodes, tree query ops, and reflection-based binding of Go structs to tree paths. Path traversal (`tree.Query`) and the typed-value cell (`tree.Value`) are defined in astral-go `api/tree` (see astral-go .ai/knowledge/api/tree.md).

## Dependencies

| Module | Why |
|---|---|
| `dir` | `OpMountRemote` resolves the target identity for remote tree mounts |
| `core/assets` | `LoadYAML` reads config and `Database()` backs the `tree__nodes` table |
| `gorm` | `DB` migrates and queries persisted tree nodes and object payloads |
| astral-go `lib/routing` | `OpRouter.AddStructPrefix` exposes tree query handlers |
| astral-go `lib/query` | tree client nodes issue `tree.get`, `tree.set`, `tree.delete`, and `tree.list` queries |
| astral-go `astral/channel` | tree ops stream values, names, acknowledgements, errors, and `EOS` with negotiated formats |
| astral-go `sig` | mount registry uses `sig.Map`; value subscriptions use `sig.Queue` and `sig.Subscribe` |

## Flows

- Module setup: `Load` reads config -> registers `Op*` handlers -> opens the database wrapper -> migrates `tree__nodes` -> mounts the DB root at `/`.
- Op delegation: `OpGet`, `OpSet`, `OpDelete`, and `OpList` each construct `treecli.NewNodeOps(mod.Root())` and forward the query; the shared handler logic (traversal, `setSingle`/`setBatch` parsing, `deleteRecursive`, Follow-mode subscription, `EOS` framing) lives in astral-go `api/tree/client` (see astral-go .ai/knowledge/api/tree.md).
- Mount splicing: `Root` returns a `NodeWrapper`; `NodeWrapper.Sub` checks each child absolute path against `mod.mounts` and substitutes mounted nodes before returning wrapped children.
- Get value: `Node.Get` loads the persisted payload or `astral.Nil`; on Follow reads the DB node pushes further value updates through its subscriber queue.
- Set value: `node.Set` upserts the payload for a resolved path, backing both single and batch handler paths.
- Delete node: `node.Delete` rejects nodes with children using `ErrNodeHasSubnodes`, closes the subscriber queue if present, and removes the row.
- Remote mount: `OpMountRemote` resolves target identity via `Dir.ResolveIdentity` -> `MountRemote` creates a remote client `Node` (astral-go `api/tree/client`) for that target -> optionally traverses the remote root with `Query(create=false)` -> `Mount` inserts it into `mod.mounts`; `OpUnmount` removes the absolute mount path.
- Struct and value binding: `Bind` walks exported struct fields -> uses `tree` tag path or snake_case field name -> either calls field `Bind` or recurses; `BindPath` queries the node then calls `Bind`. The typed-value cell it binds to (`tree.Value`) is astral-go `api/tree`.

## Source

- `mod/tree/module.go`, `bind.go`, `nil_node.go` - public interfaces, bind support (`Bind`/`BindPath`), and the nil node.
- `mod/tree/src/loader.go`, `module.go`, `deps.go`, `config.go` - construction, root mount, database setup, dependency injection, and lifecycle.
- `mod/tree/src/node.go`, `node_wrapper.go`, `db.go`, `db_node.go` - DB-backed nodes, mount substitution, persistence helpers, and schema.
- `mod/tree/src/op_get.go`, `op_set.go`, `op_delete.go`, `op_list.go`, `op_mount_remote.go`, `op_unmount.go` - query handlers (Get/Set/Delete/List delegate via `treecli.NewNodeOps(mod.Root())`).
- Traversal (`tree.Query`), the `Node`/`Value` types, errors, the remote client, and the shared `NodeOps` op handlers live in astral-go `api/tree/` (see astral-go .ai/knowledge/api/tree.md).

## Surface

| What | Why it matters |
|---|---|
| `tree.get`, `tree.set`, `tree.delete`, `tree.list` | path CRUD query surface, including follow-mode reads, single/batch set, and recursive delete |
| `tree.mount_remote`, `tree.unmount` | exposes remote subtree mounting and mount removal |
| `tree.Module` and `tree.Node` | core interfaces used by modules that bind config or expose custom tree nodes (`tree.Node` defined in astral-go `api/tree`) |
| `tree.Bind`, `tree.BindPath`, `tree.Value` | reflection and typed-value layer used for tree-backed configuration (`tree.Value` defined in astral-go `api/tree`) |
| `tree__nodes` | persisted tree namespace and object payload table |

## Invariants

- The root DB node cannot hold a value.
- Paths mounted through `Mount` and `Unmount` must be absolute; trailing slashes are trimmed.
- One mount can exist per normalized path; duplicate mount and missing unmount return errors.
- Nodes with children cannot be deleted.
- Missing node values are represented as `astral.Nil`; nil values written through `Set` are stored as `astral.Nil`.
- `pushNodeValue` is a no-op until a subscriber exists; deleting a node closes and removes its subscriber queue.
- Unknown stored object blueprints decode as `astral.UnparsedObject` through the DB helper.
- Tree streaming ops that enumerate values or child names terminate with `EOS`.
