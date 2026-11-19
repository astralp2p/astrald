# relay-contract-race

On four swarm nodes, every consumer app calls every remote provider app once,
on the provider's first available discovery update, and every call routes:
12 ordered pairs, no `route_not_found`, no retry. No node pushes a relay
contract to its siblings, so each consumer's node obtains the provider's
contract by the routing-time pull.

- **after** `adopt-node` (node1 and node2 in the User's swarm, linked) ·
  **destroys** `true` — node3 and node4 join the swarm · **config** every
  node `apphost.skip_sibling_push = true`, so no `apphost.register` pushes
  a relay contract.
- **driver** `script.py` — taps the log of every node (`log.listen`,
  `lib/nodelog.py`) and runs `driver/` (Go, this repository's astral-go) in
  `drive` mode:
  - node1 and node3, then node1 and node4, learn each other's endpoint, and
    the User adopts node3 and node4 on node1;
  - within 30 s, `nodes.links` on every node holds a link to each other node;
    2 s later, the first-link syncs have ended;
  - on every node, a consumer app granted discovery of `relay-race` opens a
    `services.discover` follow with `reach=swarm`, whose initial outcome is
    complete;
  - on every node, one after another, a provider app registers, serves `echo`
    and advertises `relay-race`;
  - each consumer, on the first available update for each provider on
    another node, calls its `echo` once, with no retry, and times the call;
  - `apphost.hosts` on every node then names each remote provider's node.
- **oracle** `verify.py` — every step held; each of the 12 ordered pairs was
  called once and answered, with no error, in under 1.5 s; each provider
  served each remote consumer exactly once; each node logged one ask for each
  remote provider's relay contracts and the contract pulled. The story holds
  again with fresh identities (`driver verify`).

This is the repro of the feedback report "Swarm discovery: first connection to
a new provider fails with route_not_found" (4 siblings, 12 pairs), with the
push dropped rather than slowed. The links are up before the apps register,
so the first-link sync (`mod/user/src/sync.go` `syncAppContracts`) carries no
provider's contract either. A consumer's node routes its first call to a
provider by asking its linked siblings for the provider's relay contracts
with `user.relay_contracts` (`mod/nodes/src/pull_relays.go`); the provider's
node answers, and the node routes through it (astral-docs
`topics/app-routing.md`, Resolving the host). Without the pull, every call
fails with `route_not_found`.

The ask lasts at most 1 s, so the call budget of 1.5 s holds the ask, the
relayed call and the provider's answer.
