# services-advertise-identity

A service an app advertises on node2 is found, on both nodes, under the app's
own identity, and nothing lets an app advertise as anyone else.

- **env** node · **start** `two-nodes` · **saves** —
- **driver** `script.py` — runs `driver/` (Go, this repository's pinned
  astral-go) in `drive` mode. Every service name starts with `sai-`.
  1. *identity*: an app on node2 advertises `sai-main` with an info string. A
     consumer app on node1 (granted discovery) finds it with `reach=swarm` as
     one offering, provided by the app and not by a node, with the info intact;
     a consumer on node2 finds it with local reach; node1's local reach finds
     nothing; the provider evaluated the consumer app and not node1.
  2. *control*: a hand-written binding that answers every ask as itself is
     accepted, so the refusals below come from what the answer claims and not
     from the binding being written without the SDK.
  3. *spoofed provider*: hand-written bindings answer with another app's
     identity, and with node2's. The node ends each binding and shows no
     offering, from node1 or on node2.
  4. *spoofed service*: a binding answers an ask for its own service with an
     offering named `sai-main`. The node ends it; discovery of both names shows
     only the honest provider.
  5. *no ServeApps*: an app whose `serve_apps` grant is revoked is refused with
     `not permitted`, and its service is not shown.
  6. *over a link*: an app of node1 cannot advertise on node2 (the query is
     rejected on its way), and node1's own identity, which does reach the op, is
     told `origin not allowed: network`. Neither service is shown.
  7. *taken names*: advertising `sai-main` a second time is refused with
     `service already advertised by this provider`, and a set that lists a
     taken name beside a fresh one is refused whole. Another app, on node1,
     advertising `sai-main` is another provider: swarm discovery from node1
     shows both under their own identities, once each, while node2 shows its
     own alone.
  8. *release*: with every binding closed, swarm discovery shows nothing.
- **oracle** `verify.py` — every step held, plus the whole story again with
  fresh identities (`driver verify`).

`services-swarm` covers discovery, follow and loss with providers it never
checks for who they are; `services-app-call` and `app-discover-swarm` find a
provider app and call it. None of them tries to advertise as someone else,
without ServeApps, or over a link, or checks that a taken name is refused.

The provider identity is stamped by the node from the binding
(`mod/services/src/coordinator/source_messages.go`, `Answer`); the SDK leaves
it empty, so only a hand-written binding can name another. Discovery does not
depend on the provider's relay contract reaching node1, and this test makes no
call to a provider, so it does not meet the route_not_found race on a first
connection to a new provider.

The flow is in Go because astral-py speaks the pre-v1 Services wire.
Spec: `.ai/system/protocols/services/ops/services.advertise.md`.
