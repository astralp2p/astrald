# services-app-call

An app on node1 discovers a provider app on node2 and calls it by the
provider's app identity, routed node1 → node2.

- **env** node · **start** `two-nodes` · **saves** —
- **driver** `script.py` — runs `driver/` (Go, this repository's astral-go) in
  `drive` mode: register a provider app on node2 that advertises `player` and
  serves a stand-in `player.state`; register a front-end app on node1 with a
  `player` discovery grant; find the provider with `reach=swarm`; call it by
  its app identity, with no relink, and check the provider saw the front-end
  as caller.
- **oracle** `verify.py` — every step held, plus the same round trip with fresh
  identities (`driver verify`).

A node routes to an app on another node through the app's relay contract.
The sibling sync pushes it when a link to a swarm member first comes up
(`mod/user/src/sync.go`, `syncAppContracts`), and `apphost.register` pushes it
to the swarm when the app registers (`mod/apphost/src/op_register.go`). The
provider here registers after the two-nodes link exists, so only the
registration's push can make it reachable from node1.
