# app-node-restart-reconnect

A provider app's node restarts, and the app reconnects with the token it
already holds and serves again: the same process re-binds, the node accepts its
stored token and its handler registration, and a caller app on another node
reaches it by its app identity.

- **after** `adopt-node` (node1 and node2 in one swarm) · **restart** `node2` ·
  **destroys** (every restarting test does) · **local_only** — only a local
  world restarts a node, and the provider is a process on the host.
- **driver** `script.py` — builds `driver/` (Go, this repository's astral-go)
  and starts `driver serve` as a long-lived app with `lib/apps.py`: it registers
  a provider app on node2 (`apphost.register`) and serves
  `app_node_restart_echo` through the SDK's `AppRegistrar`, which keeps the bind
  session and re-registers the handler whenever the session ends. The driver
  waits until the node attached the handler, calls the provider once from a
  fresh caller app on node1 (`driver call`), and records the provider's
  identity and pid, node2's restart count and endpoint, and the provider's
  state (attach and served counts). The driver then exits; the provider keeps
  running.
- **runner** — restarts node2 between driver and oracle. The restart is a
  stop and a start from the node's own root: the apphost port is the node's
  leased one (`lib/worlds/local.py` `_restart` reuses the `LocalNode` and its
  ports), so node2 comes back on the endpoint the app dials, and its token
  table, grants and contracts are read from the database.
- **oracle** `verify.py` — node2 restarted once, node1 not, and node2 kept its
  endpoint; the provider process is the same pid and alive (it reconnected, it
  was not started again). A fresh caller app on node1 calls the provider by
  its identity, retrying for 90 s, and is answered `echo, <that caller>`. The
  provider's own state shows the node completed another registration of its
  handler (`attaches` grew), that it served more queries than before, and that
  the last one was served under the new attach (`last_served_attach`).
- **recorded, not judged** — the first error the caller saw after the restart
  and the error the provider's registrar reported at the disconnect are
  printed in the oracle's log: how long the window lasts depends on the
  registrar's backoff and on the host.

The oracle runs the caller as a program (`driver call`): astral-py speaks the
pre-v1 Services wire and does not make an app-to-app query by identity, and
the answer is the one thing neither the node's state nor the provider's record
can show.

## The long-lived app

`restart = [...]` restarts nodes between driver and oracle, and a driver's own
processes end with it. This test is the first to need an app that does not, so
it introduces `tests/lib/apps.py` (see `tests/README.md`, Long-lived apps). The
provider is started there and stopped there: the oracle stops it when it
finishes, and the runner stops it at the world's close if the oracle never
got to.
