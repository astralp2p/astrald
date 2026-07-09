# mod/auth

Decides whether an identity may perform a typed action, using local action handlers first and signed delegation contracts as a fallback. Owns the typed handler registry, signed-contract signing and verification, the persistent contract+permit index used by contract fallback, and the active-contract object hold that protects indexed contracts from purge.

## Dependencies

| Module | Why |
| --- | --- |
| `crypto` | `ObjectSigner`/`TextObjectSigner` sign contracts; `VerifyObjectSignature`/`VerifyTextObjectSignature` verify `SchemeASN1`/`SchemeBIP137` |
| `objects` | `Load` resolves indexed object IDs in `OpIndex`; `RepoLocal.Scan` feeds the startup indexer; `objects.Holder` retains active contracts during purge |
| `secp256k1` | the signing engine is provided by `mod/secp256k1`; `src/signing.go` also imports the astral-go `api/secp256k1` helper `FromIdentity` directly (not injected) for key derivation |
| `core/assets` | `Database()` backs `auth__contracts` and `auth__contract_permits`; `LoadYAML` loads the (empty) `auth` config |

## Flows

* Contract authorization: no local handler granted -> recursive walk over `SignedContracts().WithSubject(actor).WithAction(action).Find` -> for each matching permit require `Delegation >= hopsBelow` and `Permit.Allows(action)` -> `action.SetActor(sc.Issuer)`, recurse with `hopsBelow+1` under a `visited` cycle guard -> first `true` grants. Authority attenuates: each issuer on the path bounds the delegation depth allowed below it.
* Index (`auth.index`): `Objects.Load` the ID -> require `*auth.SignedContract` (else `ErrInvalidContract`) -> `IndexContract` -> `Ack`.
* `IndexContract`: hold `indexMu` -> `db.contractExists` short-circuits when both signatures are already stored -> `VerifyContract` -> `db.storeSignedContract` upserts the contract row and inserts permits only when no permits exist for that row yet.
* Startup indexer: `Run` -> `indexer(ctx)` with `ZoneNetwork` excluded -> `Objects.GetRepository(RepoLocal).Scan` -> `Objects.Load` each ID -> ignore non-`*SignedContract` -> `IndexContract`.

## Invariants

* Contracts never grant directly: `Authorize` swaps `action.Actor` to the issuer and re-runs local handlers.
* The contract code path does not invoke `Contract.Allows` or `Constrainable.ApplyConstraints`; permit selection is purely the SQL join on action `ObjectType`.
* Active-contract window is `starts_at <= now AND expires_at > now`, applied identically by `findActiveContracts` and `activeContractExists`.
* `db.contractExists` requires both signatures non-empty; rows with a missing signature re-index.
* `IndexContract` is serialized by `indexMu`.
* `storeSignedContract` upserts the contract row (on conflict updates ids, sigs, expires_at) and inserts permits only when the row has no permits yet.
* `SignIssuer`/`SignSubject` refuse to overwrite an existing signature with `ErrAlreadySigned`.
* `HoldObject` fails closed: on DB error it returns true so a borderline object is not purged.
* `WithAction` takes action objects (`...astral.Object`) and indexes by `ObjectType()`, not raw strings.
* `Module.SignedContracts()` is the cross-module contract-lookup builder used by `user`.
* The sudo authorizer grants only when `Actor.IsEqual(AsID)`.
* `auth.yaml` has no fields; presence is inert.
