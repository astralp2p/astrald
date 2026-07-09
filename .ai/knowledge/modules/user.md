# mod/user

Represents the human operator across their nodes by binding the local node to a user identity through a signed swarm-membership contract. Owns the active-contract value, sibling link maintenance, local-swarm synchronization, the user asset log, and the routing/search/auth hooks that let user-owned nodes cooperate as one swarm.

## Dependencies

| Module | Why |
|---|---|
| `auth` | verifies and signs swarm contracts (`SignIssuer`, `SignSubject`, `VerifyContract`), indexes received contracts, looks up active node contracts via `SignedContracts().WithIssuer(...).WithAction(&SwarmMembershipAction{})`, and receives the `RelayForAction` and `ReadObjectAction` authorizers |
| `nodes` | `IsLinked`/`NewEnsureLinkTask` drive `MaintainLinkTask`; `UpdateNodeEndpoints` after a received node contract; `LinkClosedEvent`/`LinkCreatedEvent` events drive link maintenance and sibling sync |
| `objects` | `Store`/`Push` for signed contracts and sibling notifications; implements `Receiver`/`Holder`/`Finder` and registers the `ReadObjectAction` authorizer; `Search` preprocessor adds sibling sources |
| `scheduler` | `Ready()` gates `Run`; schedules `MaintainLinkTask` per sibling and `SyncNodesAction` on first inbound sibling link |
| `tree` | binds `/mod/user/config` (holds `ActiveContract`) and persists per-sibling sync height at `/mod/user/assets/<node>/next_height` |
| `dir` | `ResolveIdentity`, `DisplayName`, `GetAlias`/`SetAlias`; registers `localswarm` and `localuser` filters |
| `nearby` | `Broadcast` on active-contract change; `Mode` drives `ComposeStatus` (visible/adoptable/stealth attachments) |
| `apphost` | `LocalApps` enumerates apps whose contracts are pushed to siblings during sync |
| `crypto` | `crypto.Signature` carried through the membership-handshake wire protocol |
| `shell` | injected in `Deps`, currently not called |
| `core/assets` | `LoadYAML` reads the (empty) `user` config; `Database()` backs `users__assets` |

## Flows

- Active contract startup: `Run` -> wait on `Scheduler.Ready()` -> follow `Config.ActiveContract` from `/mod/user/config` -> first value goes through `onActiveContractChanged` -> close `ready` -> goroutine forwards later updates through the same handler.
- `onActiveContractChanged` (the store's apply gate): nil sets `nearby.ModeVisible`; otherwise `validateActiveContract` (both signatures, subject equals local node, not expired, grants swarm membership) -> a value that fails validation is cleared from the store so it never takes effect -> on success `Auth.IndexContract`, `Nearby.Broadcast`, and `runSiblingLinker`. No in-memory copy is kept: `ActiveContract()` reads the tree-backed value directly.
- `SetActiveContract` (write path): `validateActiveContract` for a synchronous error and to keep an invalid contract out of the store -> write into the tree-backed `ActiveContract` value; the follow applies it via `onActiveContractChanged`.
- Adopt a node (`user.adopt`): require an active contract -> require caller equals `ac.Issuer` -> resolve target via `Dir.ResolveIdentity` -> `IssueMembership` (issuer-sign, call remote `user.accept_membership`, verify returned subject signature) -> `Auth.IndexContract` -> `Objects.Store` -> `PushToLocalSwarm` -> send the signed contract.
- Accept membership (`user.accept_membership`): reject if `ActiveContract() != nil` -> receive `*auth.Contract` -> reject zero subject, non-local subject, or expiry within `minimalContractLength = 1h` -> `SwarmInvitePolicy(caller, contract)` -> receive `IssuerSig` -> `Auth.VerifyIssuer` -> `Auth.SignSubject` -> send subject signature -> `Auth.IndexContract` -> `Objects.Store` -> `SetActiveContract`.
- Accept contract (`user.accept_contract`): reject if `ActiveContract() != nil` -> receive a fully-signed `*auth.SignedContract` -> `validateActiveContract` -> `Objects.Store` -> `SetActiveContract` -> `ack`. The local-setup and cold-card counterpart of `user.accept_membership`, which runs the signing handshake instead of taking a pre-signed contract.
- Request membership (`user.request_membership`): require a local active contract -> `SwarmJoinRequestPolicy(caller)` -> `IssueMembership(ctx, caller)` (runs Flow 2 on the caller) -> `Auth.IndexContract` -> `Objects.Store` -> `PushToLocalSwarm` -> send signed contract.
- Sibling link maintenance: `runSiblingLinker` iterates `LocalSwarm()` (skipping self and already-tracked sibs) -> `NewMaintainLinkTask(target)` -> `Scheduler.Schedule`; the task runs `Nodes.NewEnsureLinkTask` with `StrategyBasic` and `StrategyTor`, retries on failure with exponential backoff capped at 15 minutes, and wakes on a `LinkClosedEvent` whose `LinkCount` is 0.
- First inbound sibling link: `ReceiveObject` observes `*events.Event` carrying `*nodes.LinkCreatedEvent` with `LinkCount == 1` from a `LocalSwarm` member -> `Scheduler.Schedule(NewSyncNodesTask(remote))` -> `SyncNodesAction.Run` calls `syncAlias`, `pushActiveContract`, `syncSiblings`, `syncApps`, then `syncAssets`.
- Asset synchronization: `syncAssets` reads/creates `/mod/user/assets/<node>/next_height` -> opens `user.sync_assets` on the remote as `ac.Issuer` -> for each `*OpUpdate` either `AddAssetWithNonce` or `RemoveAssetByNonce` (nonce-idempotent) -> on terminal `*astral.Uint64` write next height back into the tree.
- Inbound signed contract (`ReceiveObject` for `*auth.SignedContract`): accept when the contract's issuer is the active user, its subject is a swarm member, OR its issuer is a swarm member; else return `objects.ErrPushRejected` -> `Auth.VerifyContract` -> `Auth.IndexContract` -> if it `IsNodeContract`, `Nodes.UpdateNodeEndpoints(sender, subject)` and `runSiblingLinker`.
- Query preprocessing: when a contract is active, attach it to outbound queries whose caller equals the active user; if the query targets the active user, add linked siblings as relays; otherwise add `ActiveNodes(target)` as relays.
- Search preprocessing and object lookup: searches by the active user gain linked siblings as `search.Sources`; `FindObject` advertises linked siblings as candidate holders; `HoldObject` returns true for any non-removed local asset row, preserving user assets through purge.
- Nearby status (`ComposeStatus`): `ModeVisible` attaches the active contract, or otherwise attaches `Flag("adoptable")` plus a `PublicProfile`; `ModeStealth` attaches a `StealthHint` whose `Commitment` and `MaskedID` derive from `ac.Issuer`.

## Source

- `mod/user/module.go`, `swarm_join_policy.go`, `maintain_link_task.go`, `sync_nodes_action.go` - public module interface, swarm-join policy, and task interfaces. The contract helpers, swarm objects, info/notification objects, wire types, and errors live in astral-go `api/user`.
- `mod/user/src/loader.go`, `module.go`, `deps.go`, `config.go` - construction, dependency injection (including registering `RelayForAction`/`ReadObjectAction` authorizers and `localswarm`/`localuser` dir filters), lifecycle, and constants (`minimalContractLength`, `defaultContractValidity`).
- `mod/user/src/contracts.go`, `siblings.go` - active-contract state, `LocalSwarm`/`ActiveNodes`/`ActiveNodeContracts`, `IssueMembership`, sibling registry, and sibling notifications.
- `mod/user/src/maintain_link_task.go`, `sync_nodes_action.go`, `sync.go` - per-sibling link maintenance and the sibling sync orchestration (`syncAlias`, `pushActiveContract`, `syncSiblings`, `syncApps`, `syncAssets`, `PushToLocalSwarm`).
- `mod/user/src/object_receiver.go`, `object_holder.go`, `object_finder.go` - inbound contract acceptance, asset hold, and sibling-as-holder hints.
- `mod/user/src/authorizers.go`, `query_preprocessor.go`, `search_preprocessor.go`, `status_composer.go`, `swarm_policy.go` - relay/read-object auth hooks, query/search preprocessing, nearby composition, and default-accept-all policies.
- `mod/user/src/db.go`, `db_asset.go`, `assets.go` - asset row persistence and height accounting.
- `mod/user/src/op_*.go` - query operation handlers (`OpAcceptMembership`, `OpAdopt`, `OpRequestMembership`, `OpNewNodeContract`, `OpInfo`, `OpSwarmStatus`, `OpListSiblings`, `OpAssets`, `OpAddAsset`, `OpRemoveAsset`, `OpSyncAssets`, `OpSyncWith`).
- Typed user client wrappers live in astral-go `api/user/client`.

## Surface

| What | Why it matters |
|---|---|
| `user.accept_membership`, `user.accept_contract`, `user.adopt`, `user.request_membership`, `user.new_node_contract` | swarm-membership contract bootstrap and node-adoption flows |
| `user.info`, `user.swarm_status`, `user.list_siblings` | identity, swarm-membership, and live-link status query surface |
| `user.assets`, `user.add_asset`, `user.remove_asset`, `user.sync_assets`, `user.sync_with` | local asset inventory and height-ordered sibling asset sync |
| `core.QueryPreprocessor`, `objects.Search` preprocessor | attach the active contract and add local-swarm relays/sources |
| `objects.Receiver`, `objects.Holder`, `objects.Finder` | accept swarm contracts, pin active asset rows against purge, and advertise siblings as candidate holders |
| `nearby.Composer`, `dir` filters, `RelayForAction`/`ReadObjectAction` authorizers | weave user identity into presence, alias filters, relay auth, and object-read auth |
| `users__assets`, `/mod/user/config`, `/mod/user/assets/<node>/next_height` | durable asset log, tree-backed active contract, and per-sibling sync cursor |

## Invariants

- `Identity()` is nil until an active contract is accepted; it returns `ActiveContract().Issuer` (read from the tree-backed store), never the node identity.
- `LocalSwarm()` is computed from indexed `SwarmMembershipAction` contracts in `auth` (via `ActiveNodes(ac.Issuer)`); it includes the local node itself and is filtered out by `runSiblingLinker`.
- `user.accept_membership` is accepted only while there is no active contract; accepted contracts must have non-zero subject equal to the local node and at least `minimalContractLength = 1h` remaining. `user.accept_contract` shares the no-active-contract guard but validates a fully-signed contract via `validateActiveContract` (both signatures, subject equals local node, not expired, grants swarm membership).
- Default new-node contract validity is `defaultContractValidity = 365 * 24h`.
- `MaintainLinkTask`'s wake condition is `LinkClosedEvent` for the target with `LinkCount == 0`; first-link sibling sync triggers on `LinkCreatedEvent` with `LinkCount == 1`.
- Inbound `*SignedContract` is accepted when the issuer is the active user, the subject is a swarm member, or the issuer is a swarm member; everything else returns `objects.ErrPushRejected`.
- Asset rows are nonce-addressed and height-ordered; duplicate nonces are silently ignored on inbound sync.
- `HoldObject` reports true only for non-removed asset rows; removed assets no longer block purge.
- `user.assets`, `user.list_siblings`, and `user.swarm_status` stream results and terminate with `EOS`.
