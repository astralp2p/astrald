# swarm-churn-setup

The first step of the F3 churn chain (flow F3 of the E2E tests exhibit,
https://wiki.satforge.dev/doc/e2e-tests-X3LHjItA2b): four siblings each run a
long-lived mesh app, and the apps reach each other on the first attempt.

- **after** `swarm-four-siblings` · **destroys** `true` — the apps run on
  through the chain, whose steps restart, add and stop nodes ·
  **host_listener** — the mesh apps serve their handlers on the test host.
- **driver** `script.py` — spawns the `f3` mesh apps on node1-node4
  (`tests/e2e/internal/meshapp` through `lib/meshapps.py`: service `f3`,
  period 1 s, `keep_removed`) and waits up to 60 s until all 12 pairs answer;
  records the `churn_setup` fact (app ids, pids, times).
- **oracle** `verify.py` — the 12 pairs answered within 60 s, read again live;
  every pair's first call answered `ok`; every app still runs.

The chain: `swarm-churn-setup` → `swarm-churn-restart` → `swarm-churn-join` →
`swarm-churn-late-app` → `swarm-churn-leave` → `swarm-survives-churn`. Each
step performs one churn event and its oracle judges recovery from the apps'
records. The 60 s bound per step is the umbrella's decision D11.

Spec: `.ai/system/topics/app-routing.md` (Reach).
