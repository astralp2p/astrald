# swarm-churn-join

The F3 chain's join step: node5 joins the swarm with its own `f3` mesh app,
and within 60 s every pair of the five apps answers, each first call to and
from node5's app on the first attempt.

- **after** `swarm-churn-restart` · **destroys** `true` — node5 joins the
  swarm · **host_listener** — node5's mesh app serves on the test host.
- **driver** `script.py` — node1 and node5 learn each other's endpoint, the
  User adopts node5 on node1, the driver spawns node5's app and waits up to
  60 s from the adoption until all 20 pairs answer (`churn_join`).
- **oracle** `verify.py` — the 20 pairs answered within 60 s of the adoption,
  read again live; every first call to and from node5's app answered `ok`;
  every app runs.

node2-node4 never learn node5's endpoint from the driver: the adoption's
contract push and the first-hand endpoint pull carry it (as in
`swarm-four-siblings`).

Spec: `.ai/system/protocols/services/ops/services.discover.md` (Following: a
joining node contributes), `.ai/system/topics/app-routing.md` (Reach). Bound:
D11, 60 s per churn step. Tickets: https://wiki.satforge.dev/doc/0wW504QDxy,
https://wiki.satforge.dev/doc/uFXXIZXfad
