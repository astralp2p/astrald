# app-op-surface

An app on node1 serves five ops, and a second app on node1 calls them. A
query's arguments reach the serving app unaltered, and data streams both ways
over the query's channel: the argument and stream round trip an app relies on.
Each app has its own identity, token and client.

- **env** node · **start** `null` · **saves** —
- **driver** `script.py` — runs `driver/` (Go, this repository's astral-go) in
  `drive` mode:
  - the node registers the provider (`apphost.register`), and the provider
    serves its ops through `apps.ServeWith` on a client of its own token;
  - the node registers the caller, and the caller queries the provider by the
    provider's app identity, once per step below, with no retry;
  - `opsurface_echo` takes a typed struct: text with spaces, `&`, `=`, `%`,
    `+`, `;`, `?`, `#`, quotes, a backslash, a tab, capitals and non-ASCII
    letters; the smallest `int64` and the largest `uint64`; a negative
    `float64`; two bools; bytes holding zeros, `+`, `/` and `=` once encoded; an
    identity; and an optional field the caller leaves out. The provider answers
    each value as it parsed it, then the caller and the target the node
    delivered, then EOS; the caller compares all of it;
  - `opsurface_count` streams 50 integers and an EOS, which arrive in order
    and complete;
  - `opsurface_pipe` answers each integer the caller sends (50 of them, one
    at a time) and then the caller's EOS with its own;
  - `opsurface_bulk` writes 1 MiB of raw bytes, which arrive whole, with the
    same SHA-256;
  - `opsurface_sink` takes the same 1 MiB as 64 objects and an EOS, and
    answers the length and the SHA-256 it received.
- **oracle** `verify.py` — every step held, with the reason for each that did
  not, plus the same story with fresh identities (`driver verify`).

A step that fails is a fact for the oracle to judge, not a driver failure; only
a flow that cannot start (registering, serving) fails the driver. Each step
has a 30 s budget, so data lost on the way fails the step instead of hanging
it.

`mod/apphost/src/ipc_handler.go` forwards a query to the app as a
`HandleQueryMsg` that carries the query string, and copies the app's side of
the connection to the caller and the caller's side to the app. The test does
not assert anything about a query string near the `String16` limit: a longer
one is not routed to the app (`route_not_found`), which is the node's design.

The driver calls once the provider's registration hook has run: the registrar
reports `Ready` as soon as it binds, before it attaches a handler, and a hook
runs only after the attach (astral-go `lib/apps/registrar.go`).

The provider's handlers end with the driver, as `apphost.bind` removes them
when its session closes. The apps' identities, tokens and grants stay on node1.
