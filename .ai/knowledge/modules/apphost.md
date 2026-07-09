# mod/apphost

Bridges local apps into the node over IPC, an HTTP object/query gateway, and a loopback WebSocket. Owns access tokens, installed-app records, app-owned object holds, IPC and WS query handlers, and the relay contracts that let app traffic route through the host.

## Dependencies

| Module | Why |
|---|---|
| `auth` | authorizes caller override and service-handler registration with `SudoAction`, authorizes HTTP object reads with `ReadObjectAction`, signs/indexes app contracts, and finds signed contracts in the query preprocessor |
| `crypto` | `AddToIndex` stores the secp256k1 key minted by `apphost.register` so the new guest identity can sign |
| `dir` | resolves identities for configured static tokens and HTTP `@alias/path` targets; formats the host alias in `HostInfoMsg` |
| `objects` | stores signed app contracts and the guest key, serves `Objects.ReadDefault()` through HTTP `/.objects/<id>`, and discovers `Module` as an `objects.Holder` to block purge of held objects |
| `user` (opt) | `PushToLocalSwarm` republishes signed app contracts after `sign_app_contract` and `install_app`; current paths call it without a nil guard |
| `core/assets` | `Database()` backs `apphost__access_tokens`, `apphost__local_apps`, `apphost__object_holds`; `LoadYAML` loads apphost config |

## Flows

- IPC listen: `Run` -> `listen(config.Listen)` opens each `ipc.Listen` endpoint -> worker goroutines drain the conns channel -> `NewGuest(...).Serve`.
- HTTP/WS listen: `Run` -> `NewHTTPServer(mod).Run(ctx)` binds `config.BindHTTP` (empty disables); requests to `/.ws` upgrade to WebSocket, others go through bearer auth.
- Guest handshake: `Guest.Serve` sends `HostInfoMsg{Identity, Alias}` -> guest may send `AuthTokenMsg` -> `AuthenticateToken` -> `AuthSuccessMsg{GuestID}` on success or `ErrorMsg{auth_failed}` on failure. Without a token the guest stays anonymous.
- Guest query (`RouteQueryMsg`): deny if anonymous and `!AllowAnonymous` -> `blocksAnonymousWeb` confines an unauthenticated web guest (non-empty `origin-web`) to the claim-state `AnonymousWebAllowlist` (`Unclaimed` before a user exists, `Claimed` after); IPC and authenticated guests bypass it -> require `SudoAction` when `Caller` differs from `guestID` and is non-zero -> apply requested zone and filters -> anonymous loses `ZoneNetwork` -> `astral.Launch`, tag `origin-web`/`anonymous` in `Extra` -> `query.RouteInFlight` -> on accept send `QueryAcceptedMsg` and `streams.Join`; on error map to `QueryRejectedMsg` or coded `ErrorMsg`.
- Cancel: `apphost.cancel` looks up nonce in `enRoute` -> `cancel(cause)` if `Cause` set, else `cancel(nil)` -> `Ack`; unknown nonce returns an error object on the channel.
- IPC handler register (op): `apphost.register_handler` -> append `IPCHandler{Identity:q.Caller(), IpcToken, Endpoint}` to `ipcHandlers` -> `Ack` (handler persists; the op channel closes immediately).
- IPC handler register (in-band): `RegisterHandlerMsg` over the guest channel does the same after authorizing `SudoAction` for caller override, sends `Ack`, then blocks reading until the guest disconnects, at which point the handler is removed.
- Bind handler lifetime: `apphost.bind` accepts -> `Ack` -> consumes one or more `BindMsg{Token}` -> on channel close, each token triggers `removeHandlersByToken` (removes all IPC handlers with that `IpcToken`).
- WS upgrade: `handleWS` rejects non-loopback, negotiates `astral.binary.v1` or `astral.json.v1` subprotocol -> wraps the conn in `wsConn` (idempotent close) -> `NewGuestFromChannel` runs the same `Serve` protocol; `donated` flag stops the read loop when a conn is handed to a per-query routing goroutine.
- WS service register: authenticated guest sends `RegisterServiceMsg{Identity}` -> require `SudoAction` if `Identity != guestID` -> add `WSHandler{Identity, ch}` to `wsHandlers` -> spawn goroutine that removes the handler on `ctx.Done` -> `Ack`. The notification channel is the same WS the guest registered on.
- Inbound query to WS app: `Module.RouteQuery` finds an `IPCHandler` for `q.Target` first, then falls back to `WSHandler`. `WSHandler.RouteQuery` stores a `pendingInboundQuery` keyed by `q.Nonce` -> sends `IncomingQueryMsg` on the registration channel -> waits up to `QueryAttachTimeout` (5s) for `attach` (per-query WS) or `reject` (`RejectIncomingMsg` code); timeout -> route-not-found, write failure on the registration channel -> handler removed.
- WS per-query attach: guest opens a new WS, sends `AttachQueryMsg{QueryID}` -> handler finds `pendingInboundQuery` -> `Ack` -> `guest.donated.Store(true)` and the conn is sent to `pending.attach`; routing goroutine in `WSHandler.RouteQuery` reads it, proxies bytes back to the caller, and now owns close.
- WS reject: guest sends `RejectIncomingMsg{QueryID, Code}` on the registration WS -> code is forwarded to `pending.reject`; the routing goroutine returns `ErrRejected{Code}`.
- IPC inbound routing: `IPCHandler.RouteQuery` -> `ipc.DialContext(handler.Endpoint)` -> send `HandleQueryMsg` -> switch on `Ack` (accept and proxy bytes), `QueryRejectedMsg` (return `ErrRejected`), `ErrorMsg`/other (`route-not-found`); dial failure returns `errEndpointUnavailable` and the router removes the handler.
- Query preprocessing: find a signed contract `Issuer=Caller, Subject=node, Action=RelayForAction` and `Attach` it to the query; for `Issuer=Target, Action=RelayForAction`, `AddRelay(Subject)` for every non-local host.
- Access tokens: `apphost.create_token` writes a 32-char random token with default 1-year expiry; `apphost.list_tokens` streams `AccessToken` objects optionally filtered by identity and ends with `EOS`. `Prepare` reads `config.Tokens` -> resolves each identity -> deletes existing row by token -> inserts fixed token with 100-year expiry.
- Self-register (`apphost.register`): read `origin-web` -> `GetWebOriginPermits` looks up the origin's permit template -> `GetAppRegisterPolicy` gates the registration -> mint a secp256k1 key -> store key object and add to crypto index -> derive guest identity from the public key -> build and sign an app contract with 10-year duration -> index and store the signed contract -> for a trusted origin, sign and store a node->app contract granting the template permits (so the app's authority chains back through the node) -> issue a matching access token and send it.
- App contract ops: `apphost.new_app_contract` returns an unsigned `Contract{Issuer:ID, Subject:node, RelayForAction, ExpiresAt}`; `apphost.sign_app_contract` accepts a `Contract`, signs it, indexes it, stores it via `Objects.Store`, and `go User.PushToLocalSwarm`. Both default `Duration` to 1 year when zero.
- Install app: `apphost.install_app` rejects network origin -> build app contract with 1-year default -> `Auth.SignContract` (node holds both keys) -> `Auth.IndexContract` -> `Objects.Store` -> `db.CreateLocalApp` (`OnConflict{DoNothing}`) -> `go User.PushToLocalSwarm` -> send signed contract.
- Whoami: `apphost.whoami` echoes `q.Caller()` (no origin check; reports zero identity for anonymous network callers).
- Object holds: `apphost.hold_object` rejects network origin and requires non-zero caller -> `db.HoldObject(caller, id, *duration)` (nil duration -> no `hold_until`) with `OnConflict{DoNothing}`. `apphost.unhold_object` deletes only `(caller, id)` rows. `apphost.list_held_objects` streams `ObjectID` for caller rows where `hold_until IS NULL OR hold_until > now`, ending with `EOS`. `Module.HoldObject(objectID)` returns true if any active hold matches and is wired into `objects.purge` via `objects.AddHolder` auto-discovery; on lookup error it returns true (fail-safe).
- HTTP bridge: bearer token in `Authorization: Bearer ...` -> `AuthenticateToken` (401 on fail) -> set `X-Astral-Guest-Identity`/`X-Astral-Host-Identity` -> `/.objects/<id>` goes through `ReadObjectAction` + file server; other paths parse the URL into a query, choose target from `X-Astral-Target`, leading `@alias/path`, or local node, and stream the response. Preflight `OPTIONS` is short-circuited; CORS is `*`.

## Source

- `mod/apphost/module.go`, `contract.go`, `register_policy.go` - public `Module` interface, `ExtraOriginWeb`/`ExtraAnonymous` extra keys, and the `NewAppContract` contract builder.
- Wire messages (`HostInfoMsg` ... `PingMsg`), the `AccessToken`/`App` object types, `Method*` op-name constants, and the typed clients live in astral-go `api/apphost/` (see astral-go .ai/knowledge/api/apphost.md).
- `mod/apphost/src/loader.go`, `module.go`, `deps.go`, `prepare.go`, `config.go` - config, DB setup, dependency injection, static-token sync, IPC listener and worker startup, HTTP/WS startup.
- `mod/apphost/src/listen.go`, `worker.go`, `guest.go` - IPC listener fan-in, worker loop, guest protocol (handshake, route, register, attach, reject).
- `mod/apphost/src/http_server.go`, `http_object_handler.go`, `http_query_handler.go` - bearer-auth HTTP bridge and object server.
- `mod/apphost/src/ws_server.go`, `ws_handler.go`, `ws_conn.go` - WS upgrade and subprotocol negotiation, service-handler routing with per-query attach, idempotent WS close.
- `mod/apphost/src/query_router.go`, `ipc_handler.go`, `query_preprocessor.go` - inbound dispatch (IPC then WS) and relay-contract preprocessing.
- `mod/apphost/src/op_*.go` - query operation handlers: `create_token`, `list_tokens`, `register_handler`, `register` (self-register), `bind`, `cancel`, `whoami`, `new_app_contract`, `sign_app_contract`, `install_app`, `hold_object`, `unhold_object`, `list_held_objects`.
- `mod/apphost/src/access_tokens.go`, `db.go`, `db_access_token.go`, `db_local_app.go`, `db_object_hold.go`, `object_holder.go` - token, local-app, and object-hold persistence, lookups, and the `objects.Holder` adapter.

## Surface

| What | Why it matters |
|---|---|
| `apphost.create_token`, `apphost.list_tokens`, `apphost.register` | issue access tokens; `register` also mints a fresh identity and app contract |
| `apphost.register_handler`, `apphost.bind`, `apphost.cancel`, `apphost.whoami` | manage IPC query handlers and inspect/cancel in-flight queries |
| `apphost.new_app_contract`, `apphost.sign_app_contract`, `apphost.install_app` | create, sign, store, index, and register relay contracts for local apps |
| `apphost.hold_object`, `apphost.unhold_object`, `apphost.list_held_objects` | manage app-owned object holds that block purge |
| `RegisterServiceMsg`, `IncomingQueryMsg`, `AttachQueryMsg`, `RejectIncomingMsg` | WS-only inbound-query protocol for browser/JS apps |
| `Module.RouteQuery` | high-priority router that forwards target-addressed queries to IPC then WS handlers |
| `Module.PreprocessQuery` | attaches local app relay contracts and adds relay hops for apps hosted elsewhere |
| `Module.HoldObject` | `objects.Holder` adapter consulted by `objects.purge` |
| `config.Listen`, `config.BindHTTP`, `config.AllowAnonymous`, `config.Workers`, `config.Tokens` | IPC endpoints, HTTP/WS bind address, anonymous policy, worker pool, static tokens |
| `config.TrustedWebSources`, `config.AnonymousWebAllowlist` | per-origin permit templates minted on `register`, and the claim-state (`Unclaimed`/`Claimed`) op allowlist for unauthenticated web guests |
| `apphost__access_tokens`, `apphost__local_apps`, `apphost__object_holds` | persistent tokens, installed-app records, app-owned object holds |

## Invariants

- `apphost.install_app`, `apphost.register_handler`, `apphost.bind`, `apphost.hold_object`, `apphost.unhold_object`, and `apphost.list_held_objects` reject network-origin queries.
- Hold ops also require a non-zero caller; apps can list and unhold only their own rows; many apps may hold the same object.
- Anonymous IPC guests can route only when `allow_anonymous` is true and always lose `ZoneNetwork`.
- A guest can act as another identity only when `auth.Authorize(SudoAction{Actor:guestID, AsID:target})` grants it; this gate covers `Caller` override in `RouteQueryMsg`, identity in `RegisterHandlerMsg`, and identity in `RegisterServiceMsg`.
- Streaming ops (`list_tokens`, `list_held_objects`) end with `EOS`.
- The WS endpoint at `/.ws` is loopback-only; non-loopback requests receive HTTP 403. Any `Origin` is accepted; the browser Origin is attached to each routed query's `Extra` under `origin-web` (const `ExtraOriginWeb`), and a token-less session is tagged `anonymous` (const `ExtraAnonymous`), both for op-level filtering.
- An unauthenticated web guest (non-empty `origin-web`) may run only the ops on `AnonymousWebAllowlist`, selected by node claim state; IPC and authenticated guests are unrestricted. The dangerous general-purpose write path is not exposed: node setup activates its contract through `user.accept_contract`, not a raw `tree.set` of `/mod/user/config/active_contract`.
- WS auth is in-protocol via `AuthTokenMsg`, not the HTTP `Authorization` header (the latter only covers `/.objects/...` and the HTTP query bridge).
- `CreateLocalApp` and `HoldObject` use `OnConflict{DoNothing}`: reinstall does not refresh the row, and duplicate holds are idempotent. A hold does not require the object to be present locally.
- `HoldObject(objectID)` returns true on lookup error (fail-safe: purge skips the object).
- `bind_http` empty disables the HTTP and WS bridges.
- IPC handler endpoints are removed when `ipc.DialContext` fails during inbound routing; WS handlers are removed when a send on the registration channel fails.
- `apphost.register` keeps the generated private key on the node (stored as an object and indexed in crypto) before returning the token.
