# app-call-local

Two apps on node1, each with its own identity, token and client: a greeter
serves `hello`, and a visitor calls it by the greeter's app identity and is
greeted as itself.

- **env** node · **start** `null` · **saves** —
- **driver** `script.py` — runs `driver/` (Go, this repository's astral-go) in
  `drive` mode:
  - the node registers the greeter (`apphost.register`), and the greeter
    serves `hello` through `apps.ServeWith` on a client of its own token;
  - the node registers the visitor, and the visitor queries `hello` with the
    greeter's app identity as target, once; the greeter answers
    `hello, <caller>` with the caller the node delivered;
- **oracle** `verify.py` — every step held, plus the same story with fresh
  identities (`driver verify`).

`apphost.register` files a ContractRequest for `mod.auth.serve_apps_action` on
every registration (`mod/apphost/src/op_register.go`); the harness answerer
(`tests/lib/answerer.py`) approves it, and the node signs the app's contract. `apphost.register_handler` authorizes
that action (`mod/apphost/src/op_register_handler.go`). A query from one app
to another passes the apphost gate with no grant: the gate filters only
queries that target the node (`mod/apphost/src/gate.go`).

Each app runs on an `apps.AppRegistrar` built on its own client. The driver
calls the greeter once its registration hook has run: the registrar reports
`Ready` as soon as it binds, before it attaches a handler, and a hook runs
only after the attach (astral-go `lib/apps/registrar.go`). The call has no
retry.

The apps' handlers end with the driver, as `apphost.bind` removes them when
its session closes. The apps' identities, tokens and grants stay on node1.

The revoked-app step is gone: grants are, and revocations are a Non-goal of
Swarm State and User Authority (G7).
