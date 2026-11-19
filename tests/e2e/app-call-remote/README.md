# app-call-remote

A greeter app on node2 serves `hello`; a visitor app on node1 calls it by the
greeter's app identity, across the link, and is greeted as itself. Each app
has its own identity, token and client.

- **env** node · **start** `two-nodes` · **saves** —
- **driver** `script.py` — runs `driver/` (Go, this repository's astral-go) in
  `drive` mode:
  - node2 registers the greeter (`apphost.register`), and the greeter serves
    `hello` through `apps.ServeWith` on a client of its own token on node2;
  - node1 registers the visitor, and the visitor queries `hello` with the
    greeter's app identity as target, once, right after the greeter
    registered; the greeter answers `hello, <caller>` with the caller node2
    delivered;
  - after the call, `apphost.hosts` on node1 names node2 as the greeter's host.
- **oracle** `verify.py` — every step held, plus the same story with fresh
  identities (`driver verify`).

node1 routes a query for the greeter through the greeter's relay contract
(`mod/apphost/src/query_preprocessor.go`). `apphost.register` on node2 pushes
that contract to the linked siblings in a background goroutine
(`mod/apphost/src/op_register.go`, `PushToSiblings` in `mod/user/src/sync.go`),
so the call, made as soon as the greeter registers, can race the push. When
node1 holds no contract for the greeter at the call, its router asks node2 for
it with `user.relay_contracts` (`mod/nodes/src/pull_relays.go`; astral-docs
`topics/app-routing.md`, Resolving the host) and routes through node2. The
driver therefore calls once, with no wait and no retry
(https://wiki.satforge.dev/doc/tyUYbuZqVI).

node2 sees the visitor as the caller because node1 attaches the visitor's own
relay contract to the query (`query_preprocessor.go`).

The driver calls the greeter once its registration hook has run: the
registrar reports `Ready` as soon as it binds, before it attaches a handler,
and a hook runs only after the attach (astral-go `lib/apps/registrar.go`).

`services-app-call` also calls an app on node2 from node1. It reaches the
provider through swarm discovery, runs both apps in one process on the
process-global client, and retries the call for 15 s; this test runs each app
on its own client, uses no discovery, and calls once.

The greeter's handler ends with the driver, as `apphost.bind` removes it when
its session closes. The apps' identities, tokens and relay contracts stay on
node1 and node2.
