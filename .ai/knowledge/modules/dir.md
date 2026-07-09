# mod/dir

Maps human-readable names to identities and back, using a persistent alias table before a pluggable resolver chain. Owns named identity filters that can gate query targets, and publishes the local alias into nearby status when the node is visible.

## Dependencies

| Module | Why |
| --- | --- |
| `nearby` | `ComposeStatus` checks `Nearby.Mode()` and attaches `dir.Alias` when visible; injected via `core.Inject`, which fails when the module is missing |
| `astral.Node` | local identity backs `localnode` resolution, default alias setup, filter bypass for local targets, and display alias lookup |
| `core/assets` | `Database()` backs `dir__aliases`; `LoadYAML` loads the empty dir config |
| `gorm` | migrates and queries alias rows |

## Flows

- Resolve identity: empty string or `anyone` returns zero identity -> `localnode` returns the node identity -> parse literal identity -> look up `dir__aliases.alias` -> ask each resolver -> return `unknown identity`.
- Display name: zero identity returns `<anyone>` -> alias row wins -> resolver display name wins -> fingerprint fallback.
- Set alias: `SetAlias(identity, "")` deletes the alias row; non-empty alias saves a `dbAlias` row keyed by identity with a unique alias.
- Alias ops: `dir.get_alias`, `dir.set_alias`, and `dir.alias_map` expose alias lookup, mutation, and a full alias-to-identity map over query channels.
- Default alias: loader migrates the alias table -> `setDefaultAlias` checks the local node alias -> generates an `aliasgen` name when missing -> persists it and logs the name.
- Resolver registration: loader adds the DNS resolver; `AddResolver` lets other code append resolvers used after literal identity and alias lookup.
- DNS resolver: accept only lowercase domain-shaped names -> query TXT records at `_astral.<domain>` -> parse the first `id=` record as an identity.
- Filter registration: loader installs `all` and `localnode`, then sets default filters to `all`; other modules register named filters with `SetFilter` (`nodes` registers `linked`; `user` registers `localswarm` and `localuser`).
- Filter ops: `dir.filters` streams registered filter names and ends with `EOS`; `dir.apply_filters` resolves an optional ID and OR-applies the comma-separated filter names.
- Query gate: `PreprocessQuery` allows local-node targets -> reads query `filters` from `q.Extra` when provided -> otherwise uses default filters -> rejects unmatched targets with `ErrTargetNotAllowed`.
- Nearby status: `ComposeStatus` is a nearby composer -> only in `ModeVisible`, load local alias -> attach `dir.Alias` when non-empty.

## Source

- `mod/dir/module.go` - public module interface (`Module`, `Resolver`). Wire objects (`Alias`, `AliasMap`), `Method*` op-name constants, and the typed client live in astral-go `api/dir/` (see astral-go .ai/knowledge/api/dir.md).
- `mod/dir/src/loader.go`, `module.go`, `deps.go`, `config.go`, `prepare.go` - registration, dependency injection, default filters, resolver setup, and lifecycle.
- `mod/dir/src/alias.go`, `db.go` - alias CRUD and `dir__aliases` schema.
- `mod/dir/src/dns.go` - DNS resolver and domain validation.
- `mod/dir/src/query_preprocessor.go`, `status_composer.go` - target filter gate and nearby alias attachment.
- `mod/dir/src/op_get_alias.go`, `op_set_alias.go`, `op_alias_map.go`, `op_resolve.go`, `op_filters.go`, `op_apply_filters.go` - query operation handlers.
- `core/router.go`, `core/modules.go` - query-preprocessor wiring; filter propagation lives in astral-go `astral/context.go`.

## Surface

| What | Why it matters |
| --- | --- |
| `dir.get_alias`, `dir.set_alias`, `dir.alias_map`, `dir.resolve` | alias and name-resolution query methods |
| `dir.filters`, `dir.apply_filters` | query methods for inspecting and applying registered target filters |
| `Module.AddResolver` | extension point for non-alias identity resolution |
| `Module.SetFilter` and `PreprocessQuery` | extension point and enforcement path for target reachability filters |
| `nearby.Composer` implementation | publishes the local alias to nearby discovery when visible |
| `dir__aliases` | persistent identity-to-alias table |

## Invariants

- Alias table precedes resolver chain in `ResolveIdentity` and `DisplayName`.
- `DisplayName` never empty: zero returns `"<anyone>"`; otherwise fingerprint fallback.
- `SetAlias(id, "")` deletes the row.
- Local-target queries bypass filter gate.
- Empty `q.Extra["filters"]` falls back to `DefaultFilters()`; empty default allows.
- DNS resolver rejects input not matching `domainRegex` (lowercase domain shape).
- `AliasMap()` returns nil and logs verbose on DB failure.
