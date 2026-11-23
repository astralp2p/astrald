# swarm-apps-matrix-kcp

`relay-contract-race`'s matrix over kcp alone. On four siblings joined by kcp
links only, with no relay contract pushed, each of the 12 first calls answers
through the routing-time pull. This is M14 of the E2E tests exhibit, the
"transports" gap: the pull on a miss (tyUYbuZqVI) and the kcp resolver
(faKZf4rCzt, related) carried by kcp links.

- **after** `swarm-four-siblings` (four siblings, every pair linked) ·
  **destroys** `true` — tcp dial and listen go off on every node, and the
  config holds for every test in the world · **config** every node
  `apphost.skip_sibling_push = true`, so no `apphost.register` pushes a relay
  contract.
- **host_listener**: each mesh app binds its handler on the test host at its
  node's `host_ip`.
- **driver** `script.py`:
  - `lib/kcponly.py` turns tcp dial and listen off on every node, gives each
    node the others' kcp rows, closes every non-kcp link and links each pair
    over kcp;
  - it taps every node's log (`lib/nodelog.py`) and starts a mesh app on
    each node in one pass (`lib/meshapps.py`, service `m14`, `period=0`);
    each app advertises, follows with `reach=swarm` and calls each reported
    provider once;
  - it waits at most 60 s until every app finished a first call to each
    remote provider, and stops the apps;
  - it records `apps_matrix_kcp`: `{join, complete, states, links_end, logs,
    taps}`.
- **oracle** `verify.py`:
  - the join settled, and every link after the join and after the calls is
    kcp;
  - 12 of 12 first calls answered `ok`, each within 4 s;
  - each consumer called each provider once, and each provider served each
    remote consumer exactly once;
  - each consumer node logged one ask for each remote provider app's relay
    contracts and a pulled contract for it;
  - every log tap is whole.

The links are up before the apps register, so the first-link sync carries no
`m14` contract, and the push is off. A consumer's node therefore asks its
linked siblings for the provider's relay contracts on the miss, waits for the
flight at most 3 s, and routes through the provider's node (astral-docs
`topics/app-routing.md`, Resolving the host; Reach). The 4 s bound is that
wait plus a relayed round trip. The ask and pull lines are
`mod/nodes/src/pull_relays.go`'s, the needles `relay-contract-race` reads.
