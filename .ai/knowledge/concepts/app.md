# App

An `App` is an external process that uses a Host `Node` through the `apphost` module. The Host owns routing, transport, encryption, and zone enforcement; the App has no direct network presence. The App speaks the apphost protocol over an IPC bridge or a loopback WebSocket; transport mechanics live in [topics/astral-ipc](../../system/topics/astral-ipc.md) and [topics/ws-transport](../../system/topics/ws-transport.md).

## Identity

* `Identity` is the network identity the App acts as.
* `GuestID` is the identity bound to a live connection. Zero (anonymous) until authentication.
* No `AuthTokenMsg` -> `GuestID` stays zero (anonymous); the guest keeps only `allow_anonymous`-gated routing and loses `ZoneNetwork`.
* Valid `AuthTokenMsg` -> `GuestID = token.Identity`. Multiple Apps may connect with different GuestIDs.
* Static tokens come from `config.Tokens` (100-year expiry). Dynamic tokens come from `apphost.create_token` (default 1-year) or `apphost.register` (10-year).
* A guest acts as another identity only when `auth.Authorize(SudoAction{Actor:GuestID, AsID:target})` grants it. This gate governs `Caller` override on outbound queries and the identity claimed on IPC and WS handler registration.

## Handlers

* An App registers an IPC handler or a WS service handler; the Host dispatches each inbound query to it.
* A handler disappears when its connection disconnects or when `bind` releases its token.

## App record

`App` (`mod.apphost.app`) is the installed-app row `{AppID, HostID, InstalledAt}`. Stored in `apphost__local_apps` by `apphost.install_app` with `OnConflict{DoNothing}`.

## AppContract

`AppContract` is an `auth.SignedContract` with `Issuer = AppID`, `Subject = HostID` (the node), a permit granting `RelayForAction`, and `ExpiresAt` from the requested duration.

* Relay authorization: the query preprocessor attaches the contract to outbound queries whose `Caller` equals the issuer.
* Relay hints: the preprocessor adds every non-local subject of a contract issued by `Target` as a relay hop.
* Identity proof in the local swarm: `User.PushToLocalSwarm` republishes signed contracts after `sign_app_contract` and `install_app`.

Three ops produce contracts:

* `apphost.new_app_contract` returns an unsigned `Contract`.
* `apphost.sign_app_contract` signs, indexes, stores, and pushes a caller-supplied `Contract`.
* `apphost.install_app` builds, signs, indexes, stores, `CreateLocalApp`s, and pushes. Network-origin queries are rejected.

## Holds

A Hold is a row in `apphost__object_holds` keyed by `(AppID, ObjectID)`. While at least one active hold exists for an object, `objects.purge` skips it: the apphost `Module` exposes `HoldObject(objectID) bool` as an `objects.Holder` and is auto-registered by the objects module.

* `apphost.hold_object` inserts a hold for the caller with `OnConflict{DoNothing}`, so repeated holds are idempotent; `Duration` is optional (`nil` -> no expiry).
* `apphost.unhold_object` deletes only the caller's row for the object.
* Hold ops reject network origin and require a non-zero caller. Many apps may hold the same object.
