# tests/e2e/internal/swarmapps

The app side of `swarm-state.suite`, a Go tool its Python drivers run
through `tests/lib/swarmstate.py`. It is not a test: it has no `test.toml`,
so the planner never loads it. Each subcommand but `wallet` prints one JSON
line and exits 0, a failure included as `{"error": ...}`; it judges nothing.

| file | holds |
|------|-------|
| `main.go` | the subcommands and their arguments |
| `apps.go` | `register` (an app asking the hosting permits and, with `discovery`, a ServiceDiscovery permit for `wallet`), `request` (`apphost.request_contract`, waiting for the outcome), `token` (`apphost.create_token`) |
| `wallet.go` | `wallet`, the stand-in for bitcoin-wallet: it advertises `wallet` as private and answers `wallet.spend` up to 100 itself; `discover` and `call` as an app |
| `entries.go` | `entries`: every SwarmState entry a node stores, as `nodes.swarm_digest` and `nodes.swarm_fetch` return it |
| `forge.go` | `approve`: an Approval signed with a node's own key file and offered with `nodes.swarm_push`; `offer`: an entry a node stores, offered to other nodes with `nodes.swarm_push`; `membership`: a membership the User signs through `crypto.sign_hash`, offered the same way |
| `serveprobe.go` | `serve-probe`: an app's `apphost.register_handler` and a private `services.advertise`, the two acts ServeApps guards |
| `movekey.go` | `store-key`: an app's key, found among a node's stored objects, stored on another node |

`wallet` binds its handler at the node's `host_ip`, so a test that runs it
declares `host_listener`.
