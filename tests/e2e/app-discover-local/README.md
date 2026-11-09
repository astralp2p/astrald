# app-discover-local

An app on node1 finds a provider app on node1 through services discovery and
calls it; an app without a discovery grant is refused the discovery.

- **env** node · **start** fresh node1 · **saves** —
- **driver** `script.py` — runs `driver/` (Go, this repository's pinned
  astral-go) in `drive` mode:
  1. the provider app registers with ServeApps and serves itself through
     `apps.ServeWith` with its own client: it answers the `echo` op with the
     `msg` it receives and advertises the `echo-local` service;
  2. the consumer app registers, and node1's admin grants it discovery of
     `echo-local`;
  3. the consumer runs a one-shot `services.discover` of `echo-local` on
     node1 and finds the provider alone, with a complete initial outcome
     (`consumer_found_the_provider`);
  4. the consumer calls `echo` on the discovered provider identity with
     `msg=hello` and receives `hello` (`provider_echoed`); the provider saw
     the consumer as the caller (`provider_saw_the_consumer`);
  5. a stranger app with no discovery grant runs the same discovery and is
     refused with `not permitted to discover`, with no offering
     (`stranger_refused`).
- **oracle** `verify.py` — every step held, plus the whole story again with
  fresh identities (`driver verify`).

Each app runs on a client of its own: `apps.Serve` and `apps.WithServices`
use process-global defaults, so the provider serves through `apps.ServeWith`
with a registration hook that advertises through its own services client.
The service name is unique to this test, so no other test in the world
observes the provider.

The flow is in Go because astral-py speaks the pre-v1 Services wire.
Spec: `.ai/system/protocols/services/ops/services.discover.md`.
