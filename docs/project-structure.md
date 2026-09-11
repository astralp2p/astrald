# Project Structure

## Directory Layout

```
astrald/
├── .ai/             Agent workspace; the entry point is .ai/AGENTS.md
├── brontide/        Noise XK handshake (Noise_XK_secp256k1_ChaChaPoly_SHA256)
├── cmd/             Binaries: astrald and the astral-* tools
├── core/            Node implementation, routing, module system
├── debug/           Crash log capture
├── docs/            Project documentation
├── lib/             Small libraries: aliasgen, apphost-js, arl, paths
├── mobile/          gomobile-bind entry point for Android and iOS hosts
├── mod/             Pluggable modules (31)
├── resources/       Named resource store with file and in-memory backends
├── scripts/         Blueprint check and astral-query shell completion
├── tasks/           Task runner and task groups
└── tests/           Integration tests (see tests/README.md)
```

Primitives, wire types, and client libraries live in [astral-go](https://github.com/astralp2p/astral-go), not in this repository.

## Module Structure

A module lives in `mod/<name>/`. The package in `mod/<name>/` holds the module's public API; `mod/<name>/src/` holds its implementation. `mod/all` has no `src/`: it imports every module.

```
src/
├── module.go        Module implementation
├── loader.go        Bootstrap and module registration
├── deps.go          Dependency declarations
├── config.go        Configuration structures and defaults, when the module has configuration
├── db.go            Database access, when the module has a database
├── op_*.go          Ops served as queries
└── object_*.go      Object-system providers
```

## Available Modules

Descriptions of modules with a protocol in [astral-docs](https://github.com/astralp2p/astral-docs/blob/master/README.md) follow its index.

| Module | Description |
|--------|-------------|
| [all](../mod/all/README.md) | Imports every available module |
| [apphost](../mod/apphost/src/README.md) | On-device API for local apps (tokens, handlers, contracts, holds) |
| archives | Archive handling |
| auth | Capability contracts, signing, and action authorization |
| bip137sig | BIP-39/32/137 seed, key derivation, and message signing |
| [coldcard](../mod/coldcard/README.md) | Coldcard hardware wallet as a BIP-0137 signer |
| crypto | Signing and verifying hashes and text, public key derivation |
| dir | Alias management, identity resolution |
| ether | LAN UDP broadcast for node presence and discovery |
| events | Event system |
| exonet | External network: dispatches a dial to the dialer registered for the endpoint's network |
| fs | Filesystem operations |
| [fwd](../mod/fwd/src/README.md) | TCP forwarding and tunnels |
| gateway | Gateway operations |
| indexing | Indexers that track object membership across named repositories |
| ip | IP utilities |
| kcp | KCP protocol transport |
| log | Logging system |
| [mcp](../mod/mcp/src/README.md) | AI agent registration and the MCP endpoint serving agents the network |
| nat | NAT traversal via UDP hole punching |
| nearby | Local network discovery |
| nodes | Encrypted links and multiplexed sessions between nodes |
| objects | Typed object storage, retrieval, and provider discovery |
| scheduler | Task scheduling |
| secp256k1 | secp256k1 ECDSA signing, including an ASN.1 hash signer |
| services | Service registry that fans discovery out to every registered discoverer |
| shell | Shell operations |
| tcp | TCP networking |
| tor | Onion-service transport over Tor |
| tree | Hierarchical key-value configuration store |
| user | User identity, swarm membership, asset list |

## Logic extensions

### Ops (`op_*.go`)

A module method named `Op<Name>` implements the op `<module>.<name>`: `OpGetAlias` in `mod/dir` serves `dir.get_alias`. The module's `loader.go` registers these methods with `mod.ops.AddStructPrefix(mod, "Op")`; `mod/gateway` registers them in `deps.go` through `mod.router.AddStructPrefix(mod, "Op")`. Op documentation lives in astral-docs under `protocols/<module>/ops/`.

**Pattern** (`mod/dir/src/op_get_alias.go`):
```go
type opGetAliasArgs struct {
	ID  *astral.Identity `query:"required"`
	Out string
}

func (mod *Module) OpGetAlias(ctx *astral.Context, q *routing.IncomingQuery, args opGetAliasArgs) (err error) {
	ch := q.Accept(channel.WithOutputFormat(args.Out))
	defer ch.Close()
	// ...
}
```

An op receives a `routing.IncomingQuery` (astral-go `lib/routing`), accepts or rejects it, and exchanges objects over the resulting channel. The query's arguments arrive parsed into the args struct: `query:"required"` marks a mandatory argument, and `query:"key:<name>"` sets an argument's name.

### Object system (`object_*.go`)

A module provides an object-system capability in a file named after the method it implements:

| File | Method | Implemented by |
|------|--------|----------------|
| `object_describer.go` | `DescribeObject` | archives, fs, nodes |
| `object_finder.go` | `FindObject` | nodes, user |
| `object_holder.go` | `HoldObject` (`objects.Holder`) | apphost, auth, crypto, user |
| `object_opener.go` | `OpenObject` | archives |
| `object_receiver.go` | `ReceiveObject` (`objects.Receiver`) | nat, nearby, nodes, scheduler, user |
| `object_searcher.go` | `SearchObject` | archives, fs |

`mod/crypto/src/object_signer.go` is outside this set: it holds `ObjectSigner`, the crypto module's object signer.
