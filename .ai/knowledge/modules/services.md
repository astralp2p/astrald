# mod/services

Aggregates service advertisements from registered discoverers into a local stream and can mirror another node's service stream into a cached database. Owns service-discoverer registration, `services.discover` fan-in semantics, `services.sync` remote cache refresh, and the `services__services` table keyed by provider and service name.

## Dependencies

| Module | Why |
|---|---|
| `dir` | `OpSync` resolves the requested provider identity before opening the remote discovery stream |
| `core/assets` | loader creates the database handle and migrates `services__services` |
| `core.Node` | `LoadDependencies` scans loaded modules and auto-registers implementations of `services.Discoverer` |

## Flows

- Discoverer registration: `LoadDependencies` walks loaded modules -> skips itself -> type-asserts `services.Discoverer` -> `AddDiscoverer` inserts into the set.
- Local aggregate: `DiscoverServices` calls each registered discoverer -> logs and skips failing sources -> concurrent snapshot fan-in emits updates until each source sends nil or closes.
- Follow aggregate: after all snapshots complete and `follow` is true -> send nil separator -> fan in remaining live updates until sources close or context is done.
- Discover operation: `services.discover` accepts a channel -> calls `DiscoverServices` with caller and follow flag -> sends each `*services.Update` -> converts nil separator to `EOS` -> sends final `EOS`.
- Sync operation: `services.sync` resolves provider ID -> runs under a network-zone child context -> any inbound message cancels the sync -> `syncServices` pulls remote updates -> sends `Ack` or an error object.
- Remote sync storage: `syncServices` opens `services.discover` on the provider -> deletes all cached rows for that provider -> creates rows for available updates and deletes rows for unavailable updates.
- The `client.Discover` snapshot/`EOS`/nil-separator/follow semantics live in astral-go `api/services/client` (see astral-go .ai/knowledge/api/services.md).

## Source

- `mod/services/module.go` - public interfaces (`Module`, `Discoverer`), `ModuleName`, and `DBPrefix`. The `Update` wire object and `Method*` op-name constants live in astral-go `api/services/` (see astral-go .ai/knowledge/api/services.md).
- `mod/services/src/loader.go`, `mod/services/src/deps.go`, `mod/services/src/module.go` - database setup, router setup, discoverer auto-registration, and sync entry point.
- `mod/services/src/discover_services.go` - fan-in of snapshot and follow streams from registered discoverers.
- `mod/services/src/op_discover.go`, `mod/services/src/op_sync.go` - query handlers for discovery and remote sync.
- `mod/services/src/db.go`, `mod/services/src/db_service.go` - database model and provider service create, delete, and lookup helpers.

## Surface

| What | Why it matters |
|---|---|
| `services.discover` | streams the local aggregate of service updates, with `EOS` separating snapshot and live phases |
| `services.sync` | refreshes the local database cache from a remote provider's discovery stream |
| `services.Discoverer` | extension point used by modules such as NAT to advertise availability |
| `services__services` | cached provider service table with unique provider and service-name rows |

## Invariants

- The database uniqueness constraint is on service name and provider identity.
- `syncServices` deletes all cached services for a provider before applying the remote stream.
- A nil update from a discoverer marks the snapshot/follow separator, not a service value.
- `OpSync` runs the remote discovery under `ZoneNetwork` and cancels when the accepted channel receives any object.
- `LoadDependencies` never registers the services module as its own discoverer.
- A discoverer that returns an error from `DiscoverServices` is logged at verbosity 2 and skipped for that aggregate call.
