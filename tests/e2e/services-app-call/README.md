# services-app-call

An app on node1 discovers a provider app on node2 and calls it by the
provider's app identity, routed node1 → node2.

- **env** node · **start** `two-nodes` · **saves** —
- **driver** `script.py` — runs `driver/` (Go, this repository's astral-go) in
  `drive` mode: register a provider app on node2 that advertises `player` and
  serves a stand-in `player.state`; register a front-end app on node1 with a
  `player` discovery grant; find the provider with `reach=swarm`; show that a
  call to its app identity does not route yet; close and reopen the node1–node2
  link; call again and check the provider saw the front-end as caller.
- **oracle** `verify.py` — every step held, plus the same round trip with fresh
  identities (`driver verify`).

A node routes to an app on another node through the app's relay contract,
which the sibling sync pushes when a link to a swarm member first comes up
(`mod/user/src/sync.go`, `syncAppContracts`). The provider here registers after
the two-nodes link exists, so the test reopens the link to run that sync.
