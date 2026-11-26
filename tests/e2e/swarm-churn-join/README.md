# swarm-churn-join

The F3 chain's join step: node5 joins the swarm with its own `f3` mesh app,
and within 60 s every pair of the five apps answers, each first call to and
from node5's app on the first attempt.

- **after** `swarm-churn-restart` · **destroys** `true` — node5 joins the
  swarm · **host_listener** — node5's mesh app serves on the test host.
- **driver** `script.py` — node1 and node5 learn each other's endpoint,
  node5 joins through node1 as a Member, the driver spawns node5's app and
  waits up to 60 s from the join until all 20 pairs answer (`churn_join`).
- **oracle** `verify.py` — the 20 pairs answered within 60 s of the join,
  read again live; every first call to and from node5's app answered `ok`;
  every app runs.

node2-node4 never learn node5's endpoint from the driver: SwarmState sync
brings them node5's membership, and each pulls node5's endpoints from a
linked member (`onNewMember`, `mod/user/src/module.go:165-182`;
`LearnMemberEndpoints`, `mod/nodes/src/endpoints_changed.go:173-217`), as in
`swarm-four-siblings`.

Spec: `.ai/system/protocols/services/ops/services.discover.md` (Following: a
joining node contributes), `.ai/system/topics/app-routing.md` (Reach). Bound:
D11, 60 s per churn step. Tickets: https://wiki.satforge.dev/doc/0wW504QDxy,
https://wiki.satforge.dev/doc/uFXXIZXfad
