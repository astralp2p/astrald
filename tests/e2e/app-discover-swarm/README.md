# app-discover-swarm

An app on node1 finds a provider app on node2 through services discovery with
`reach=swarm` and calls it on node2.

- **env** node · **start** `two-nodes` · **saves** —
- **driver** `script.py` — runs `driver/` (Go, this repository's pinned
  astral-go) in `drive` mode:
  1. the provider app registers on node2 with ServeApps and serves itself
     through `apps.ServeWith` with its own client: it answers the `echo` op
     with the `msg` it receives and advertises the `echo-swarm` service;
  2. the consumer app registers on node1, and node1's admin grants it
     discovery of `echo-swarm`;
  3. the consumer runs a one-shot `services.discover` of `echo-swarm` with
     `reach=swarm` and finds the provider alone, with a complete initial
     outcome (`consumer_found_the_provider`);
  4. `apphost.hosts` on node1 names node2 alone as the provider's host
     (`node1_knows_the_host`);
  5. the consumer calls `echo` once on the discovered provider identity with
     `msg=hello` and receives `hello` from node2 (`provider_echoed`); the
     provider saw the consumer as the caller (`provider_saw_the_consumer`).
- **oracle** `verify.py` — every step held, plus the whole story again with
  fresh identities (`driver verify`).

A discovery update does not wait for the provider's relay contract: the
contract reaches node1 by the best-effort push of `apphost.register`
(`mod/apphost/src/op_register.go`), on its own goroutine. Until discovery
pushes before it emits, a call made right after discovery can fail with
`route_not_found`, so the driver polls `apphost.hosts` on node1 until it
names node2, for up to 60 s, and only then calls, exactly once.

`services-app-call` covers the same route with one process-global app and a
call it retries for 15 s; here each app runs on a client of its own through
`apps.ServeWith`, and the call is made once, after `apphost.hosts` names the
host. The service name is unique to this test, so no other test in the world
observes the provider.

The flow is in Go because astral-py speaks the pre-v1 Services wire.
Spec: `.ai/system/protocols/services/ops/services.discover.md`.
