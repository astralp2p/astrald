# app-two-providers

Two provider apps, one on node1 and one on node2, offer the same service; an
app on node1 finds both through services discovery with `reach=swarm`, calls
each, and each provider answers its own call alone.

- **env** node · **start** `two-nodes` · **saves** —
- **driver** `script.py` — runs `driver/` (Go, this repository's pinned
  astral-go) in `drive` mode:
  1. a provider app registers on node1 and another on node2, each with
     ServeApps; each serves itself through `apps.ServeWith` with its own
     client, answers the `echo` op with the `msg` it receives, and
     advertises the `echo-pair` service;
  2. the consumer app registers on node1, and node1's admin grants it
     discovery of `echo-pair`;
  3. the consumer runs a one-shot `services.discover` of `echo-pair` with
     `reach=swarm` and finds exactly the two providers, with a complete
     initial outcome (`consumer_found_both`);
  4. `apphost.hosts` on node1 names node1 alone as the host of node1's
     provider and node2 alone as the host of node2's provider
     (`node1_knows_both_hosts`);
  5. the consumer calls `echo` once on each discovered provider identity,
     with `msg=hello near` for node1's provider and `msg=hello far` for
     node2's, and each call returns its own message (`each_provider_echoed`);
  6. each provider answered exactly one call: its own message, from the
     consumer (`each_provider_answered_its_own_call`).
- **oracle** `verify.py` — every step held, plus the whole story again with
  fresh identities (`driver verify`).

A discovery update does not wait for a sibling provider's relay contract: the
contract reaches node1 by the best-effort push of `apphost.register`
(`mod/apphost/src/op_register.go`), on its own goroutine. Until discovery
pushes before it emits, a call made right after discovery can fail with
`route_not_found`, so the driver polls `apphost.hosts` on node1 until it
names each provider's node, for up to 60 s per provider, and only then calls
each provider, exactly once.

The service name is unique to this test, so no other test in the world
observes the providers.

The flow is in Go because astral-py speaks the pre-v1 Services wire.
Spec: `.ai/system/protocols/services/ops/services.discover.md`.
