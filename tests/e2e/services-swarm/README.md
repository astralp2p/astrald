# services-swarm

An app on node1 discovers services on node1 and, with `reach=swarm`, on node2,
which evaluates its providers for the app.

- **env** node · **start** `two-nodes` · **saves** —
- **driver** `script.py` — runs `driver/` (Go, this repository's astral-go)
  in `drive` mode: scope refusal, local reach, a two-service binding, swarm
  one-shot and follow, Change, a member provider's loss and return, a silent
  provider's incomplete outcome, and an unregistered service's bare `eos`.
- **oracle** `verify.py` — every step held, plus one independent swarm round
  trip with fresh identities (`driver verify`).

The flow is in Go because astral-py still speaks the pre-v1 Services wire.
Spec: `.ai/system/protocols/services/ops/services.discover.md`.
