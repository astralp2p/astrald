# Project Structure

## Directory Layout

```
astrald/
├── astral/          Core types and utilities (Identity, Object, Query, Router)
├── brontide/        Noise XK protocol implementation
├── core/            Node implementation, routing, module system
├── lib/             Client libraries (apphost, query, routers, ipc)
├── mod/             Pluggable modules (30+)
├── sig/             Thread-safe collections (Map, Set, Queue, Ring, Pool)
├── streams/         Stream utilities (pipes, readers, writers)
├── tasks/           Task management
└── cmd/             Binary entry points (astrald, anc, etc.)
```

## Module Structure

Each module follows a standard layout in `mod/<module>/src/`:

```
src/
├── module.go        Main module implementation
├── loader.go        Module initialization
├── deps.go          Dependency declarations
├── config.go        Configuration structures
├── db.go            Database operations
├── op_*.go          Shell command implementations
└── object_*.go      Object system plugin implementations
```

## Available Modules

| Module | Description |
|--------|-------------|
| all | Module registry for build inclusion |
| allpub | Public module registry |
| [apphost](../mod/apphost/src/README.md) | App hosting interface |
| archives | Archive handling |
| auth | Authentication |
| crypto | Cryptographic operations and key management |
| dir | Directory and name resolution |
| ether | Ethernet layer |
| events | Event system |
| exonet | External network |
| fs | Filesystem operations |
| [fwd](../mod/fwd/src/README.md) | TCP forwarding and tunnels |
| gateway | Gateway operations |
| [ip](../mod/ip/README.md) | IP utilities |
| kcp | KCP protocol transport |
| log | Logging system |
| media | Media indexing |
| nat | NAT traversal |
| nearby | Local network discovery |
| nodes | Node operations and streams |
| [objects](../mod/objects/README.md) | Object management |
| scheduler | Task scheduling |
| shell | Shell operations |
| tcp | TCP networking |
| tor | Tor integration |
| user | User management |

## Logic extensions 

### Shell operations (`op_*.go`)

Files prefixed with `op_` implement shell commands accessible through the query interface.

**Pattern:**
```go
// op_describe.go
type opDescribeArgs struct {
    ObjectID string `query:"optional"`
}

func (mod *Module) OpDescribe(ctx *astral.Context, q shell.Query, args opDescribeArgs) error {
    // Command implementation
}
```

**Examples:**
- `op_describe.go` - Describe objects
- `op_search.go` - Search operations
- `op_resolve.go` - Resolve identities
- `op_new_stream.go` - Create streams

Operations use `shell.Query` for I/O and take parsed arguments via struct tags.

### Object system (`object_*.go`)

The objects module provides a plugin architecture. Modules implement these interfaces by convention:

| Interface | Implemented by |
|-----------|----------------|
| **Describer** (`object_describer.go`) | fs, archives, media, nodes |
| **Finder** (`object_finder.go`) | user, nodes |
| **Holder** (`object_holder.go`) | user, nodes |
| **Receiver** (`object_receiver.go`) | user, nodes, nearby, scheduler |
| **Searcher** (`object_searcher.go`) | fs, archives |

### Other Conventions

| File | Purpose |
|------|---------|
| `module.go` | Core module logic and interface implementations |
| `deps.go` | Lists required module dependencies |
| `loader.go` | Bootstrap and module registration |
| `config.go` | Configuration structures and defaults |
| `db.go` | Database access and persistence |
