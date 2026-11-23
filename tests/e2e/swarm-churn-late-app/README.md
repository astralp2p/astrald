# swarm-churn-late-app

The F3 chain's late-app step: an app registers on node1 while node2 is down,
and once node2 returns, node2's app reaches the late app on its first call;
every pair of the six apps answers again within 60 s.

- **after** `swarm-churn-join` · **restart** `["node2"]` · **destroys**
  `true` — node2 restarts and a sixth app joins · **host_listener** — the
  late app serves on the test host.
- **driver** `script.py` — records the restart counts and its own end time
  (`churn_late_app`).
- **while_down** `while_down.py` — while node2 is down, spawns the `f3-late`
  mesh app on node1 (service `f3`), waits until it registered and
  advertised, then 12 s more, past the first retry of its registration push.
- **oracle** `verify.py` — node2 restarted once and the others not; from the
  oracle's start every pair of the six apps answers within 60 s; node2's
  app's first call to the late app answered `ok`; every app runs. It prints
  the recovery time.

Spec: `.ai/system/topics/app-routing.md` (Reach). Bound: D11, 60 s per churn
step. Ticket: https://wiki.satforge.dev/doc/qArE8Rfm7T
