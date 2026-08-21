# mod/tree

Maintains a path-addressed namespace of astral objects backed by a database node table and live value subscriptions. Owns mount points for local or remote nodes, tree query ops, and reflection-based binding of Go structs to tree paths. Path traversal (`tree.Query`) and the typed-value cell (`tree.Value`) are defined in astral-go `api/tree`.

## Dependencies

| Module | Why |
|---|---|
| `dir` | `OpMountRemote` resolves the target identity for remote tree mounts |
| `core/assets` | reads config and backs the `tree__nodes` table |
| `gorm` | migrates and queries persisted tree nodes and object payloads |
| astral-go `lib/routing` | `OpRouter.AddStructPrefix` exposes tree query handlers |
| astral-go `astral/channel` | tree ops stream values, names, acknowledgements, errors, and `EOS` |
| astral-go `sig` | mount registry uses `sig.Map`; value subscriptions use `sig.Queue`/`sig.Subscribe` |

## Invariants

* The root DB node cannot hold a value.
* Paths mounted through `Mount`/`Unmount` must be absolute; trailing slashes are trimmed.
* One mount can exist per normalized path; duplicate mount and missing unmount return errors.
* Nodes with children cannot be deleted.
* Missing node values are `astral.Nil`; a nil written through `Set` is stored as `astral.Nil`.
* `pushNodeValue` is a no-op until a subscriber exists; deleting a node closes and removes its subscriber queue.
* Unknown stored object blueprints decode as `astral.UnparsedObject`.
* Tree streaming ops that enumerate values or child names terminate with `EOS`.
