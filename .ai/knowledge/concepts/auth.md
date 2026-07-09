# Auth

Authorization answers: "may this action's actor perform this action?" `Authorize` returns `bool`; denial does not throw. This note covers the node-side dispatch in `mod/auth`; action, `Contract`, `Permit`, and `SignedContract` types live in astral-go `api/auth`.

## Grant Quorum

* Any handler returning `true` grants.
* All handlers must deny for denial.
* Handlers are registered per action type, keyed by `ActionObject.ObjectType()`.

## Contract Delegation

If no local handler grants, `Authorize` walks a delegation chain of active signed contracts (`mod/auth/src/authorize.go`):

* The walk starts with `hopsBelow = 0` and a `visited` set seeded with the original actor (cycle guard).
* At each step the local handlers run for the current actor; a `true` grants.
* Otherwise it looks up active `SignedContract`s where `Subject == actor` and some permit's `Action == action.ObjectType()`.
* For each matching permit the link must permit the delegation happening below it: `int(Permit.Delegation) >= hopsBelow` (the link closest to the actor needs none; `Delegation == 0` is non-delegable).
* `Permit.Allows(action)` is checked at every link, so a chain can only narrow.
* When a permit qualifies, the walk re-enters as the contract's `Issuer` with `hopsBelow + 1`, adding the issuer to `visited`; the actor is restored on the way out.
* First `true` anywhere in the chain grants.

Authority attenuates along the chain: every issuer on the path bounds the delegation depth and the constraints, so authority can only shrink downward.

## SudoAction

* Built-in action type (`mod/auth/sudo_action.go`) requesting permission for `Actor` to act as `AsID`.
* The registered authorizer (`AuthorizeSudo`) grants only when `Actor.IsEqual(AsID)`; cross-identity sudo is reachable only through contract delegation.

## Contract Query

* `Module.SignedContracts()` returns a fluent builder with `WithIssuer`, `WithSubject`, and `WithAction` (action filter by `ObjectType()`).
* `Find(ctx)` returns active signed contracts (window `starts_at <= now < expires_at`), decoding signatures and permits from `auth__contracts` + `auth__contract_permits`.

## Signing And Verification

* `Module.SignIssuer` / `Module.SignSubject` refuse to overwrite an existing signature with `ErrAlreadySigned`.
* `Module.SignContract` signs issuer then subject.
* Scheme choice lives in `mod/crypto`: `Sign` prefers an object (hash) signature (`SchemeASN1`) and falls back to a text signature (`SchemeBIP137`) when no object-signing engine is available for the key; `Verify` dispatches on `Signature.Scheme`.

## Object Holding

* `auth` holds active indexed signed-contract objects against `objects.purge` via the `objects.Holder` hook, so authorization keeps working after a purge cycle.
* The hold window matches the active-contract lookup window (`starts_at <= now < expires_at`).
