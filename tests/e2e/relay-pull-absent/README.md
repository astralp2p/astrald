# relay-pull-absent

A call to an app identity no node hosts fails as route not found within about
1 s, and a second call within 30 s asks the siblings nothing.

- **after** `adopt-node` (node1 and node2 in the User's swarm, linked).
- **driver** `script.py` — taps node1's log (`log.listen`, `lib/nodelog.py`)
  and runs `driver/` (Go, this repository's astral-go): an app on node1 calls
  `ping` on a freshly generated identity twice, back to back, and times each
  answer.
- **oracle** `verify.py` — both calls fail as route not found, each in under
  1.1 s; node1 logged one ask for the identity and one miss.

node1 holds no relay contract for the identity, so its router asks node2 with
`user.relay_contracts` on the first call (`mod/nodes/src/pull_relays.go`).
node2 holds none either and answers `eos` alone. The ask ends with a miss that
node1 keeps for 30 s, so the second call fails without an ask (astral-docs
`topics/app-routing.md`, Resolving the host).
