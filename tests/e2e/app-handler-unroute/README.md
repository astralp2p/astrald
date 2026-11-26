# app-handler-unroute

When an app's session ends, node1 stops routing queries to its handler. A
visitor app calls other apps on node1 by their app identity; in each removal
leg the call reaches the handler before the session ends, and fails with
`route_not_found` after, and keeps failing. When a failed dial removes the
handler of an app bound through an `AppRegistrar`, node1 ends that app's bind
session, and the app's handler is routed to again once it is registered anew.

- **env** node · **start** `null` · **saves** —
- **driver** `script.py` — runs `driver/` (Go, this repository's astral-go) in
  `drive` mode. Each leg registers a fresh app on node1 and ends its session
  in one way:
  - `clean_bind_close_unroutes` — the app's `apphost.bind` channel closes while
    its IPC endpoint keeps listening. The endpoint would still answer a dial, so
    only the bind cleanup can unroute the handler.
  - `clean_app_stop_unroutes` — an app served through `apps.ServeWith` stops
    (its context ends).
  - `crash_app_unroutes` — the same kind of app runs in a child process of the
    driver (re-executed through `os.Executable()`) and is killed with SIGKILL.
    The bind session and the endpoint both die, so either removal path may
    unroute it.
  - `crash_bind_drop_unroutes` — a child process holds only the bind session of
    a handler whose endpoint the driver keeps listening on, and is killed. The
    node sees the session drop while the endpoint still answers, so only the
    bind cleanup can unroute the handler.
  - `failed_dial_removes` — a handler registered with no bind session. Its
    endpoint closes and a call fails with `route_not_found`; the endpoint then
    comes back at the same address with the same token, and calls still fail.
    The failed dial removed the handler for good; it did not merely skip it.
  - `listener_restart_reregisters` — a handler registered and bound through an
    astral-go `AppRegistrar`. Its listener closes and a call fails with
    `route_not_found`; the listener comes back at the same address with the
    same token 200 ms after the close, and a call reaches it within 5 s. The
    failed dial ended the app's bind session, so the registrar registered the
    handler again.
  - `bystander_unaffected` — a bystander app served on node1 answers after
    every leg, so only the ended session's handler was removed.
- Every handler binds TCP at node1's `host_ip` (the test declares
  `host_listener`); `failed_dial_removes` and `listener_restart_reregisters`
  listen again on the port their first bind resolved.
- **oracle** `verify.py` — every leg held, plus the same story with fresh
  identities (`driver verify`).

Two removal paths exist in `mod/apphost/src/query_router.go`:
`removeHandlersByToken` removes the handlers a bind session named with a
`BindMsg` when the session ends (`op_bind.go`), and `RouteQuery` removes an IPC
handler whose dial fails (`errEndpointUnavailable`) and ends the bind session
that bound its token (`app_bind.go`). The legs that keep the
endpoint listening tell the first from the second; `failed_dial_removes` guards
the second on its own.

The polling deadline for a removal is 5 s; on a quiet host the first call after
the session ends already fails. The apps' identities and tokens stay on
node1; their handlers end with the legs.
