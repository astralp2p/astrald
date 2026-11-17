# services-swarm-membership

An open `reach=swarm` follow on node1 tracks the swarm member set: a node
adopted after the follow opened contributes its offering, and the offering is
removed when the node is expelled. The follow is never reopened.

- **env** node · **start** `two-nodes` · **destroys** — `ssm_late` is expelled.
- **driver** `script.py` — links the fresh node `ssm_late` to node1, then runs
  `services-swarm/driver` in `membership` mode: a follow on node1, the
  adoption of `ssm_late`, and its expulsion.
- **oracle** `verify.py` — both steps held.

Spec: `.ai/system/protocols/services/ops/services.discover.md`.
