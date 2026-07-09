# mod/user

Represents the human operator across their nodes by binding the local node to a user identity through a signed swarm-membership contract. Owns the active-contract value, sibling link maintenance, local-swarm synchronization, the user asset log, and the routing/search/auth hooks that let user-owned nodes cooperate as one swarm.

## Dependencies

| Module | Why |
|---|---|
| `auth` | `SignIssuer`/`SignSubject`/`VerifyContract` and `IndexContract` for swarm contracts; `SignedContracts().WithIssuer(...).WithAction(&SwarmMembershipAction{})` looks up active node contracts; registers the `RelayForAction` and `ReadObjectAction` authorizers |
| `nodes` | `IsLinked`/`NewEnsureLinkTask` drive `MaintainLinkTask`; `UpdateNodeEndpoints` after a received node contract; `LinkClosedEvent`/`LinkCreatedEvent` drive link maintenance and sibling sync |
| `objects` | `Store`/`Push` for signed contracts and sibling notifications; implements `Receiver`/`Holder`/`Finder` and registers the `ReadObjectAction` authorizer; `Search` preprocessor adds sibling sources |
| `scheduler` | `Ready()` gates `Run`; `Schedule` runs `MaintainLinkTask` per sibling and `SyncNodesAction` on first inbound sibling link |
| `tree` | binds `/mod/user/config` (holds `ActiveContract`) and persists per-sibling sync height at `/mod/user/assets/<node>/next_height` |
| `dir` | `ResolveIdentity`, `DisplayName`, `GetAlias`/`SetAlias`; registers `localswarm` and `localuser` filters |
| `nearby` | `Broadcast` on active-contract change; `Mode` drives `ComposeStatus` |
| `apphost` | `LocalApps` enumerates apps whose contracts are pushed to siblings during sync |
| `crypto` | `crypto.Signature` carried through the membership-handshake wire protocol |
| `shell` | injected in `Deps`, currently not called |
| `core/assets` | `LoadYAML` reads the (empty) `user` config; `Database()` backs `users__assets` |

## Flows

* Active-contract apply gate (`onActiveContractChanged`): nil sets `nearby.ModeVisible`; otherwise `validateActiveContract` (both signatures, subject equals local node, not expired, grants swarm membership) -> a value that fails validation is cleared from the store so it never takes effect -> on success `Auth.IndexContract`, `Nearby.Broadcast`, and `runSiblingLinker`. No in-memory copy is kept: `ActiveContract()` reads the tree-backed value directly.
* Accept membership (`user.accept_membership`) handshake order: reject if `ActiveContract() != nil` -> receive `*auth.Contract` -> reject zero subject, non-local subject, or expiry within `minimalContractLength = 1h` -> `SwarmInvitePolicy` -> receive `IssuerSig` -> `Auth.VerifyIssuer` -> `Auth.SignSubject` -> send subject signature -> `Auth.IndexContract` -> `Objects.Store` -> `SetActiveContract`.
* First inbound sibling link: `ReceiveObject` observes a `*nodes.LinkCreatedEvent` with `LinkCount == 1` from a `LocalSwarm` member -> `Scheduler.Schedule(NewSyncNodesTask(remote))` -> `SyncNodesAction.Run` calls `syncAlias`, `pushActiveContract`, `syncSiblings`, `syncApps`, then `syncAssets`.

## Invariants

* `Identity()` is nil until an active contract is accepted; it returns `ActiveContract().Issuer` (read from the tree-backed store), never the node identity.
* `LocalSwarm()` is computed from indexed `SwarmMembershipAction` contracts in `auth` (via `ActiveNodes(ac.Issuer)`); it includes the local node itself and is filtered out by `runSiblingLinker`.
* `user.accept_membership` is accepted only while there is no active contract; accepted contracts must have a non-zero subject equal to the local node and at least `minimalContractLength = 1h` remaining. `user.accept_contract` shares the no-active-contract guard but validates a fully-signed contract via `validateActiveContract` (both signatures, subject equals local node, not expired, grants swarm membership).
* Default new-node contract validity is `defaultContractValidity = 365 * 24h`.
* `MaintainLinkTask`'s wake condition is `LinkClosedEvent` for the target with `LinkCount == 0`; first-link sibling sync triggers on `LinkCreatedEvent` with `LinkCount == 1`.
* Inbound `*SignedContract` is accepted when the issuer is the active user, the subject is a swarm member, or the issuer is a swarm member; everything else returns `objects.ErrPushRejected`.
* Asset rows are nonce-addressed and height-ordered; duplicate nonces are silently ignored on inbound sync.
* `HoldObject` reports true only for non-removed asset rows; removed assets no longer block purge.
* `user.assets`, `user.list_siblings`, and `user.swarm_status` stream results and terminate with `EOS`.
