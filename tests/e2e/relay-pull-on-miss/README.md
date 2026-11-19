# relay-pull-on-miss

A node that holds no relay contract for an app obtains it from a linked
sibling on its first call to the app, and routes the call.

- **after** `adopt-node` (node1 and node2 in the User's swarm, linked) ·
  **config** node2 `apphost.skip_sibling_push = true`, so node2's
  `apphost.register` pushes no relay contract to node1.
- **driver** `script.py` — taps node1's log (`log.listen`, `lib/nodelog.py`)
  and runs `driver/` (Go, this repository's astral-go) in `drive` mode:
  - node1 registers a visitor app; node2 registers the greeter app, which
    serves `hello` through `apps.ServeWith` on a client of its own token;
  - `apphost.hosts` on node1 names no host for the greeter: node1 holds no
    relay contract for it;
  - the visitor queries `hello` with the greeter's app identity as target,
    once; the greeter answers `hello, <visitor>`, and the call returns in
    under 1.5 s;
  - `apphost.hosts` on node1 then names node2 for the greeter.
- **oracle** `verify.py` — every step held; node1 logged one ask for the
  greeter and the greeter's contract pulled from node2; the story holds again
  with fresh identities (`driver verify`).

node1's router finds no relay for the greeter and asks its linked siblings for
the greeter's relay contracts with `user.relay_contracts`
(`mod/nodes/src/pull_relays.go`, `mod/user/src/relay_fetch.go`). node2 answers
the contract it signed at registration (`mod/user/src/op_relay_contracts.go`);
node1 admits it by `AdmitRelayContract`, indexes it, and routes through node2
(astral-docs `topics/app-routing.md`, Resolving the host). Without the pull the
call fails with `route_not_found`.

The ask lasts at most 1 s, so the call budget of 1.5 s holds the ask, the
relayed call and the greeter's answer.
