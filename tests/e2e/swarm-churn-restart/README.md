# swarm-churn-restart

The F3 chain's restart step: node2 restarts under the running `f3` mesh apps,
and every pair answers again within 60 s, node2's app re-registering in the
same process.

- **after** `swarm-churn-setup` · **restart** `["node2"]` · **destroys**
  `true` — the chain continues the restarted world · **host_listener** — the
  mesh apps serve on the test host.
- **driver** `script.py` — records the restart counts, the attaches of
  node2's app and its own end time (`churn_restart`); the harness then
  restarts node2.
- **oracle** `verify.py` — node2 restarted once and the others not; from the
  oracle's start (node2 runs again), every pair answers within 60 s; node2's
  app has the pid `swarm-churn-setup` recorded and attached its handler
  again; every app runs. It prints the recovery time.

Spec: `.ai/system/topics/external-providers.md` (re-registration),
`.ai/system/core-definitions/query.md` (Routing outcomes, retries). Bound:
D11, 60 s per churn step. Tickets: https://wiki.satforge.dev/doc/agqhgxZZrQ,
https://wiki.satforge.dev/doc/f9YkqOEMAD,
https://wiki.satforge.dev/doc/R6mPZCTZfh
