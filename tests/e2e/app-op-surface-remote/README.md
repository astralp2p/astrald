# app-op-surface-remote

`app-op-surface` across the link. A provider app on node2 serves four ops, and a
caller app on node1 calls them by the provider's app identity. A query's typed
arguments reach the serving app unaltered, a stream of 50 objects and an EOS
arrives in order, and 1 MiB crosses the link intact in each direction. Each app
has its own identity, token and client.

- **env** node · **start** `two-nodes` · **saves** —
- **driver** `script.py` — runs `driver/` (Go, this repository's astral-go) in
  `drive` mode:
  - node2 registers the provider (`apphost.register`), and the provider serves
    its ops through `apps.ServeWith` on a client of its own token on node2;
  - the driver waits, within 30 s, until `apphost.hosts` on node1 names node2
    for the provider (see below);
  - node1 registers the caller, and the caller queries the provider's ops by
    the provider's app identity, once each, with no retry, in the network zone:
    - `opsurface_echo` takes a typed struct: text with spaces, `&`, `=`, `%`,
      `+`, `;`, `?`, `#`, quotes, a backslash, a tab, capitals and non-ASCII
      letters; the smallest `int64` and the largest `uint64`; a negative
      `float64`; two bools; bytes holding zeros, `+`, `/` and `=` once encoded;
      an identity; and an optional field the caller leaves out. The provider
      answers each value as it parsed it, then the caller and the target the
      node delivered, then EOS;
    - `opsurface_count` streams 50 integers and an EOS;
    - `opsurface_bulk` writes 1 MiB of raw bytes to the caller;
    - `opsurface_sink` takes the same 1 MiB as 64 objects and an EOS, and
      answers the length and the SHA-256 it received.
- **oracle** `verify.py` — holds the values that were sent and compares them
  with what the driver recorded each call returned: every argument as sent, the
  caller and target as the app identities (not the nodes'), the integers
  `0..49` in order with EOS, and the length and SHA-256 of the 1 MiB in both
  directions. A failure names every call that went wrong, with the driver's
  reason for each.

The oracle does not rerun the story: the driver records the values it received
and the oracle judges them against constants it holds itself, so a call that
alters data fails the oracle even though the driver exits 0. A call that fails
is a fact for the oracle to judge, not a driver failure; only a flow that
cannot start (registering, serving, node1 never learning the provider) fails
the driver. Each call has a 30 s budget, so data lost on the way fails the call
instead of hanging it.

This test leaves `app-op-surface` the single-node legs that only matter on one
node (the lockstep `opsurface_pipe`), and `app-call-remote` the `hello` check.

node1 routes a query for the provider through the provider's relay contract
(`mod/apphost/src/query_preprocessor.go`) and sends it to node2 as a relay
query (`mod/nodes/src/mux.go`, `handleRelayQuery`), so node2 sees the caller
app as the caller. `apphost.register` on node2 pushes that contract to the
linked siblings in a background goroutine (`mod/apphost/src/op_register.go`,
`PushToSiblings` in `mod/user/src/sync.go`), so a call made as soon as the
provider registers can race the push and fail with `route_not_found`. The wait
for `apphost.hosts` is the workaround, as in `app-call-remote`
(https://wiki.satforge.dev/doc/swarm-discovery-first-connection-to-a-new-provider-fails-with-route_not_found-taD5d1d8Zn),
until push-before-emit lands.

The driver calls once the provider's registration hook has run: the registrar
reports `Ready` as soon as it binds, before it attaches a handler, and a hook
runs only after the attach (astral-go `lib/apps/registrar.go`).

The provider's handlers end with the driver, as `apphost.bind` removes them
when its session closes. The apps' identities, tokens and relay contracts stay
on node1 and node2.
