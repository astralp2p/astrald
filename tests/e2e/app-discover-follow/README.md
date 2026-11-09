# app-discover-follow

An app on node1 follows a service across the swarm before any provider
exists; a provider app starts on node2, the follow shows it, and the app
calls it; the provider stops, and the follow removes it.

- **env** node · **start** `two-nodes` · **saves** —
- **driver** `script.py` — runs `driver/` (Go, this repository's pinned
  astral-go) in `drive` mode:
  1. the consumer app registers on node1, node1's admin grants it discovery
     of `echo-follow`, and the consumer opens a `services.discover` follow of
     `echo-follow` with `reach=swarm`; the first event is a complete initial
     outcome with no offering (`follow_starts_empty`);
  2. the provider app registers on node2 with ServeApps and serves itself
     through `apps.ServeWith` with its own client: it answers the `echo` op
     with the `msg` it receives and advertises `echo-follow`; the next follow
     event is the provider's available offering of `echo-follow`
     (`follow_shows_the_provider`);
  3. `apphost.hosts` on node1 names node2 alone as the provider's host
     (`node1_knows_the_host`);
  4. the consumer calls `echo` once on the provider identity the follow
     showed, with `msg=hello`, and receives `hello` from node2
     (`provider_echoed`); the provider saw the consumer as the caller
     (`provider_saw_the_consumer`);
  5. the provider stops: its advertisement and its handler close; the next
     follow event removes exactly the provider's offering of `echo-follow`
     (`follow_removes_the_provider`).
- **oracle** `verify.py` — every step held, plus the whole story again with
  fresh identities (`driver verify`).

A follow event does not wait for the provider's relay contract: the contract
reaches node1 by the best-effort push of `apphost.register`
(`mod/apphost/src/op_register.go`), on its own goroutine. Until discovery
pushes before it emits, a call made right after the follow shows the provider
can fail with `route_not_found`, so the driver polls `apphost.hosts` on node1
until it names node2, for up to 60 s, and only then calls, exactly once.

Each follow event has 60 s to arrive. `services-swarm` covers a follow and a
member provider's loss without calling the provider; here the consumer calls
the provider the follow showed. The service name is unique to this test, so
no other test in the world observes the provider.

The flow is in Go because astral-py speaks the pre-v1 Services wire.
Spec: `.ai/system/protocols/services/ops/services.discover.md`.
