# Auth

Authorization answers: "may this action's actor perform this action?"

`Authorize` returns `bool`. Denial does not throw.

Grant rule:

* Any handler returning `true` grants.
* All handlers must deny for denial.

The action, `Contract`, `Permit`, and `SignedContract` wire types (and the
`Constrainable` interface) now live in astral-go `api/auth` (see astral-go
.ai/knowledge/concepts/auth.md). Structure and field-level specs are in
astral-docs [protocols/auth/types](../../system/protocols/auth/types). This note
covers only the node-side dispatch in `mod/auth`.

## Handlers

* Handlers are registered per action type; the type key is `ActionObject.ObjectType()`.
* `TypedHandler` exposes `ActionType() string`.
* `Func[T ActionObject]` adapts a typed callback and type-asserts the incoming object to `T`, returning `false` on mismatch (`mod/auth/actions_map.go`).

## Contract Delegation

If no local handler grants, `Authorize` walks a delegation chain of active
signed contracts (`mod/auth/src/authorize.go`):

* The walk starts with `hopsBelow = 0` and a `visited` set seeded with the
  original actor (cycle guard).
* At each step, the local handlers run for the current actor. A `true` grants.
* Otherwise it looks up active `SignedContract`s where `Subject == actor` and
  some permit's `Action == action.ObjectType()` (`Contract.HasPermit`).
* For each matching permit, the link must permit the delegation happening below
  it: `int(Permit.Delegation) >= hopsBelow` (the link closest to the actor needs
  none; `Delegation == 0` is non-delegable). The permit's constraints must also
  pass — `Permit.Allows(action)` is checked at every link, so a chain can only
  narrow.
* When a permit qualifies, the walk re-enters as the contract's `Issuer` with
  `hopsBelow + 1`, adding the issuer to `visited`; the actor is restored on the
  way out.
* First `true` anywhere in the chain grants.

Authority attenuates along the chain: every issuer on the path bounds the
delegation depth and the constraints, so authority can only shrink downward.

## Built-Ins

`SudoAction`:

* Built-in action type (`mod/auth/sudo_action.go`).
* Requests permission for `Actor` to act as `AsID`.
* The registered authorizer (`AuthorizeSudo` in `mod/auth/src/authorizers.go`)
  grants only when `Actor.IsEqual(AsID)`; cross-identity sudo is reachable only
  through contract delegation.

`ContractQueryBuilder`:

* Fluent builder returned by `Module.SignedContracts()`.
* `WithIssuer(*Identity)`, `WithSubject(*Identity)`, `WithAction(...astral.Object)` (action filter is by `ObjectType()`).
* `Find(ctx)` returns active signed contracts (window: `starts_at <= now < expires_at`), decoding signatures and permits from `auth__contracts` + `auth__contract_permits`.

## Signing And Verification

* `Module.SignIssuer` / `Module.SignSubject` refuse to overwrite an existing signature with `ErrAlreadySigned`.
* `Module.SignContract` runs issuer then subject.
* `signAs` delegates to `Crypto.Sign`; `verifySig` delegates to `Crypto.Verify`
  (`mod/auth/src/signing.go`).
* The scheme choice lives in `mod/crypto`: `Sign` prefers an object (hash)
  signature (`SchemeASN1`) and falls back to a text signature (`SchemeBIP137`)
  when no object-signing engine is available for the key; `Verify` dispatches on
  `Signature.Scheme`.

## Object Holding

Active indexed signed-contract objects are held by `auth` against `objects.purge` via the `objects.Holder` hook, so authorization keeps working after a purge cycle. The hold window matches the active-contract lookup window.
