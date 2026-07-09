# mod/apphost

Bridges local apps into the node over IPC, an HTTP object/query gateway, and a loopback WebSocket. Owns access tokens, installed-app records, app-owned object holds, IPC and WS query handlers, and the relay contracts that let app traffic route through the host.

## Dependencies

| Module | Why |
|---|---|
| `auth` | `Authorize(SudoAction{...})` gates caller override and handler registration; `Authorize(ReadObjectAction{...})` gates HTTP object reads; `SignContract`/`IndexContract` sign and index app contracts; `SignedContracts().Find` supplies relay contracts to the query preprocessor |
| `crypto` | `AddToIndex` stores the secp256k1 key minted by `apphost.register` so the new guest identity can sign |
| `dir` | `ResolveIdentity` for configured static tokens and HTTP `@alias/path` targets; formats the host alias in `HostInfoMsg` |
| `objects` | `Store` persists signed app contracts and the guest key; `ReadDefault()` serves objects through HTTP `/.objects/<id>`; `AddHolder` discovers `Module` as an `objects.Holder` to block purge of held objects |
| `user` (opt) | `PushToLocalSwarm` republishes signed app contracts after `sign_app_contract` and `install_app`; current paths call it without a nil guard |
| `core/assets` | `Database()` backs `apphost__access_tokens`, `apphost__local_apps`, `apphost__object_holds`; `LoadYAML` loads apphost config |

## Flows

* Guest handshake ordering: `Guest.Serve` sends `HostInfoMsg` -> guest may send `AuthTokenMsg` -> `AuthSuccessMsg` on success, `ErrorMsg{auth_failed}` on failure. Without a token the guest stays anonymous.
* WS inbound query state machine: `WSHandler.RouteQuery` stores a `pendingInboundQuery` keyed by `q.Nonce` -> sends `IncomingQueryMsg` on the registration channel -> waits up to `QueryAttachTimeout` (5s) for an attach or reject. Timeout -> route-not-found. Send failure on the registration channel removes the handler.
* WS per-query attach: the guest opens a new WS and sends `AttachQueryMsg{QueryID}` -> the handler matches the `pendingInboundQuery`, sets `guest.donated`, and hands the conn to the routing goroutine, which then owns close and proxies bytes to the caller. `RejectIncomingMsg{QueryID, Code}` on the registration WS instead returns `ErrRejected{Code}`.
* IPC inbound routing: `IPCHandler.RouteQuery` dials `handler.Endpoint` and sends `HandleQueryMsg` -> `Ack` proxies bytes, `QueryRejectedMsg` returns `ErrRejected`, other replies return route-not-found. Dial failure returns `errEndpointUnavailable` and the router removes the handler.
* Self-register (`apphost.register`): mint a secp256k1 key -> store the key object and add it to the crypto index -> derive the guest identity from the public key -> build and sign a 10-year app contract -> index and store it -> for a trusted origin, additionally sign and store a node->app contract granting the origin's permit template, so the app's authority chains back through the node -> issue and send a matching access token.

## Invariants

* `apphost.install_app`, `apphost.register_handler`, `apphost.bind`, `apphost.hold_object`, `apphost.unhold_object`, and `apphost.list_held_objects` reject network-origin queries.
* Hold ops also require a non-zero caller; an app can list and unhold only its own rows; many apps may hold the same object.
* Anonymous IPC guests can route only when `allow_anonymous` is true and always lose `ZoneNetwork`.
* A guest acts as another identity only when `auth.Authorize(SudoAction{Actor:guestID, AsID:target})` grants it; this gate covers `Caller` override in `RouteQueryMsg`, identity in `RegisterHandlerMsg`, and identity in `RegisterServiceMsg`.
* An unauthenticated web guest (non-empty `origin-web`) may run only the ops on `AnonymousWebAllowlist`, selected by node claim state (`Unclaimed` before a user exists, `Claimed` after); IPC and authenticated guests are unrestricted. The general-purpose write path is deliberately not exposed: node setup activates its contract through `user.accept_contract`, not a raw `tree.set` of `/mod/user/config/active_contract`.
* The WS endpoint `/.ws` is loopback-only; non-loopback requests receive HTTP 403. WS auth is in-protocol via `AuthTokenMsg`, not the HTTP `Authorization` header (the header only covers `/.objects/...` and the HTTP query bridge).
* `CreateLocalApp` and `HoldObject` persist with `OnConflict{DoNothing}`: reinstall does not refresh the row, duplicate holds are idempotent, and a hold does not require the object to be present locally.
* `Module.HoldObject(objectID)` returns true on lookup error (fail-safe: purge skips the object).
* `bind_http` empty disables the HTTP and WS bridges.
* IPC handler endpoints are removed when `ipc.DialContext` fails during inbound routing; WS handlers are removed when a send on the registration channel fails.
* `apphost.register` keeps the generated private key on the node (stored as an object and indexed in crypto) before returning the token.
* `Module.PreprocessQuery` attaches a signed contract with `Issuer=Caller, Subject=node, Action=RelayForAction` to the query, and for a contract with `Issuer=Target, Action=RelayForAction` calls `AddRelay(Subject)` for every non-local host.
* `Module.RouteQuery` finds an `IPCHandler` for `q.Target` before falling back to a `WSHandler`.
* Streaming ops (`apphost.list_tokens`, `apphost.list_held_objects`) end with `EOS`.
