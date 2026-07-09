# mod/auth

Decides whether an identity may perform a typed action, using local action handlers first and signed delegation contracts as a fallback. Owns the typed handler registry, signed-contract signing/verification, the persistent contract+permit index used by contract fallback, and the active-contract object hold that protects indexed contracts from purge.

## Dependencies

| Module | Why |
| --- | --- |
| `crypto` | `ObjectSigner`/`TextObjectSigner` for contract signing; `VerifyObjectSignature`/`VerifyTextObjectSignature` for `SchemeASN1`/`SchemeBIP137` verification |
| `objects` | `Load` resolves indexed object IDs in `OpIndex`; `RepoLocal.Scan` feeds the startup indexer; `objects.Holder` is implemented to retain active contracts during purge |
| `secp256k1` | key derivation for signing/verifying contract signatures uses the astral-go `api/secp256k1` package helper `FromIdentity` (imported directly in `src/signing.go`, not injected as a module dependency); the secp256k1 signing engine itself is still provided by `mod/secp256k1` |
| `core/assets` | `Database()` backs `auth__contracts` and `auth__contract_permits`; `LoadYAML` loads the (empty) `auth` config |

## Flows

- Direct authorization: `Authorize(ctx, action)` runs every handler registered for `action.ObjectType()` -> first `true` grants.
- Contract authorization: no local handler granted -> recursive walk over `SignedContracts().WithSubject(actor).WithAction(action).Find` -> for each matching permit require `Delegation >= hopsBelow` and `Permit.Allows(action)` (constraints narrow at every link) -> `action.SetActor(sc.Issuer)`, recurse with `hopsBelow+1` under a `visited` cycle guard -> first `true` grants. Authority attenuates: each issuer on the path bounds the delegation depth allowed below it.
- Register handler: `Add(handlers...)` reads `TypedHandler.ActionType()` per handler -> appends to the `sig.Map` slot for that type via `handlers.Replace`.
- Sudo handler: loader registers `auth.Func[*auth.SudoAction](AuthorizeSudo)`; grants only when `action.Actor().IsEqual(action.AsID)`.
- Sign contract op (`auth.sign_contract`): receive `*auth.Contract` on the channel -> wrap in `SignedContract` -> `SignContract` (`SignIssuer` then `SignSubject`) -> send the signed object or `astral.Err`.
- Sign one side: `SignIssuer` / `SignSubject` refuse overwrite with `ErrAlreadySigned` -> `signAs` derives the key via `secp256k1.FromIdentity` -> try `Crypto.ObjectSigner` (ASN1); on failure, fall back to `Crypto.TextObjectSigner` (BIP137).
- Index op (`auth.index`): `Objects.Load` the ID with `ReadDefault()` -> require `*auth.SignedContract` (else `ErrInvalidContract`) -> `IndexContract` -> send `Ack`.
- `IndexContract`: `indexMu` -> resolve `ObjectID` -> `db.contractExists` short-circuits when both signatures are already stored -> `VerifyContract` -> `db.storeSignedContract` upserts the contract row and inserts permits only when no permits exist for that row yet.
- Startup indexer: `Run` -> `indexer(ctx)` with `ZoneNetwork` excluded -> `Objects.GetRepository(RepoLocal).Scan` -> `Objects.Load` each ID -> ignore non-`*SignedContract` -> `IndexContract`.
- Object hold: `HoldObject(id)` -> `db.activeContractExists(id)` (same `starts_at <= now < expires_at` window) -> true keeps the object alive in purge; DB error fails closed by returning true.
- Contract lookup: `contractQuery` filters by `issuer_id`, `subject_id`, active time window, and (when `WithAction` was called) joins `auth__contract_permits` on `name IN actions` with distinct contracts; `Find` decodes signatures and permits back into `*SignedContract`.

## Source

- `mod/auth/module.go`, `actions_map.go`, `sudo_action.go` - public `Module` and `ContractQueryBuilder` interfaces, typed handler registry, and sudo action. The action base type, contract/permit/signed-contract wire types, and sentinels live in astral-go `api/auth`.
- `mod/auth/src/loader.go`, `module.go`, `deps.go`, `config.go`, `prepare.go` - module construction, router setup, dependency injection, DB migration, and lifecycle.
- `mod/auth/src/authorize.go`, `authorizers.go` - dispatch loop, contract fallback, and built-in sudo authorizer.
- `mod/auth/src/signing.go` - `SignIssuer`/`SignSubject`/`SignContract` and per-scheme verification.
- `mod/auth/src/contracts.go`, `contract_query.go`, `object_holder.go` - `IndexContract`, repository indexer, active-contract query builder, and `HoldObject` hook.
- `mod/auth/src/op_sign_contract.go`, `op_index.go` - query operation handlers.
- `mod/auth/src/db.go`, `db_contract.go`, `db_contract_permit.go` - GORM rows, active-contract lookup, upsert with conditional permit insert, and permit encode/decode.
- `mod/auth/views/` - signed-contract view helpers. Typed client wrappers live in astral-go `api/auth/client`.

## Surface

| What | Why it matters |
| --- | --- |
| `Module.Authorize`, `Module.Add` | central extension point: typed actions are registered with `Func[T]` handlers and dispatched by `ObjectType()` |
| `Module.SignIssuer`, `Module.SignSubject`, `Module.SignContract` | per-side and combined contract signing using the available crypto scheme |
| `Module.VerifyIssuer`, `Module.VerifySubject`, `Module.VerifyContract`, `Module.IndexContract` | verification and durable indexing of received signed contracts |
| `Module.SignedContracts()` | builder used by `Authorize` and by other modules (e.g. `user`) to look up active contracts by issuer/subject/action |
| `objects.Holder` | active indexed signed-contract objects are pinned against purge |
| `auth.sign_contract`, `auth.index` | query methods for signing a contract and indexing an already-stored signed contract |
| `auth__contracts`, `auth__contract_permits` | persistent index searched on contract fallback |

## Invariants

- Contracts never grant directly: `Authorize` swaps `action.Actor` to the issuer and re-runs local handlers.
- The contract code path does not invoke `Contract.Allows` or `Constrainable.ApplyConstraints`; permit selection is purely the SQL join on action `ObjectType`.
- Active-contract window is `starts_at <= now AND expires_at > now`, applied identically by `findActiveContracts` and `activeContractExists`.
- `db.contractExists` requires both signatures non-empty; rows with a missing signature re-index.
- `IndexContract` is serialized by `indexMu`.
- `storeSignedContract` upserts the contract row (on conflict updates ids, sigs, expires_at) and inserts permits only when the row has no permits yet.
- `SignIssuer`/`SignSubject` refuse to overwrite an existing signature with `ErrAlreadySigned`.
- `HoldObject` fails closed: on DB error it returns true so a borderline object is not purged.
- `WithAction` takes action objects (`...astral.Object`) and indexes by `ObjectType()`, not raw strings.
- `auth.yaml` has no fields; presence is inert.
