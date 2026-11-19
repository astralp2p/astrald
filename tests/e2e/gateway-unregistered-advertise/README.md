# gateway-unregistered-advertise

A node advertises `gw:<gateway>:<itself>` only while its registration with
the gateway is up.

- **env** netsim · **start** `gateway-relink` · **saves** — · **mutates** true
- **steps** `stop-astrald reflector`, then the driver.
- **driver** `script.py` — poll node2's `nodes.resolve_endpoints localnode`
  until no gw row through the reflector is left.
- **oracle** `verify.py` — node2's resolver, read live, lists no gw row
  through the stopped gateway.

## The other half is in gateway-relay

gateway-relay's oracle checks that node2, registered, resolves
`gw:<reflector>:<node2>` for itself with a 7 d TTL. This test checks that the
row goes once the registration is lost. Together they pin the advertise rule:
the gateway module sets a node's registration flag after `gateway.node_register`
succeeds and clears it when the idle connection pool to the gateway ends
(`mod/gateway/src/maintain_gateway_connections_task.go`).

## How the registration ends

`stop-astrald` stops astrald on the reflector and leaves the VM up. node2's
idle connections to the gateway close, the pool's redials are refused, and
after three consecutive failures the pool returns `ErrSocketUnreachable`
(`mod/gateway/src/pool.go`). The driver records how long node2 took to drop
the row in `cleared_after_s`.

## What it does not cover

Siblings. A sibling that pulled node2's endpoints while the registration was
up keeps the gw row until its TTL ends; this test reads node2's own resolver
only.
