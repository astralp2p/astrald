# gateway-relay

A node nobody can dial is reached through a gateway that can.

- **env** netsim · **start** `two-nodes` · **saves** — · **mutates** true
- **steps** `enter-nat node2`, `expose-apphost node2`,
  `configure-gateway node2 --gateway reflector`, then the driver.
- **driver** `script.py` — wait for node2 to register itself, give node1 a
  `gw:` endpoint and nothing else, link.
- **oracle** `verify.py` — the link to node2 is a `gw` link and not a `tcp`
  one, and node2 answers for itself over it.

The gateway module is a rendezvous and relay, five ops, and nothing exercised
any of them.

## The third node was already there

The task document expected this test to need a new
`tests/netsim/labs/three-node/` lab, and called that "likely the larger half of
the work". It is not needed. The `two-node` lab already builds **three** VMs —
node1, node2 and the `reflector` that `nat-punch` bounces a public address off
— and the roster is only the manifest's `nodes` list. Naming `reflector` there
gives it a pushed astrald, a session and a tunnel like any other node.

Measured before this test was written: a three-name roster yields three
endpoints and three distinct identities, and `gateway.node_list` on the
reflector answers an empty list rather than an error, so the module is live and
reachable. The lab is unchanged by this branch.

## Registration is configuration, not a call

Both halves are config and neither is reachable over apphost, which is why
`configure-gateway` is a vmop rather than driver calls:

- a node is a gateway only with `gateway.enabled: true`;
- a node registers with one only by listing it under `gateways`, after which
  the module schedules `MaintainGatewayConnectionsTask` and registers itself.

So the driver **waits** for node2 to arrive in the gateway's list rather than
putting it there. A test that registered node2 by hand would prove the op
works, not that a configured node arrives.

## Why NAT

Claim three is the one that keeps the others honest: traffic has to cross the
relay because it has no alternative, not because it happened to. On a LAN where
every node can dial every other, a gateway test that only shows a node
answering proves nothing.

`enter-nat` gives the asymmetry the story needs — node2 can open a connection
outwards, to the gateway, and nothing can open one to it. `leave-lan` would not
do: a node that has left the LAN cannot reach the gateway either.

## The link's network is the verdict

Read live from node1 and **filtered to node2**. That filter is load-bearing:
node1 holds an ordinary `tcp` link to the gateway itself, so an unfiltered
"no tcp links" check would trip over the very hop that makes the test work.

`gw` present means the relay carried it. `tcp` present, to a node behind NAT,
would mean a direct path survived and the world was never what the test
claimed.

## What it took to make this pass

Green at `d43def01`, in about thirteen minutes from the cached `two-nodes`
stage. Getting there took seven netsim runs and one loopback lab, and none of
the blockers were where they looked:

- **The gateway opens its own listener on the port it is configured with.**
  Naming astrald's tcp port (1791) collides: the tcp module loses the bind,
  inbound links land on the gateway's socket-claim handler instead of the link
  handshake, and every dial dies as `unexpected EOF`. The relay gets 1795.
- **`gateway.networks.tcp.endpoint` does nothing.** It is parsed with
  `Exonet.Parse` during the gateway's own `LoadDependencies`, racing
  `mod/tcp`'s registration of the `tcp` parser (`core/modules.go` runs them
  concurrently). The parse fails `unsupported network`, the error is logged and
  swallowed, and the module falls back to `IP.PublicIPCandidates()`.
- **That fallback is empty on a lab network,** because `IsPublic()` is
  `IsGlobalUnicast() && !IsPrivate()` and 10.77 is RFC1918. Every registration
  is then refused `no public IP available for gateway network tcp` — in band,
  so the routing layer logs a clean `routed in 152µs` and only the caller ever
  learns why. The fix is `mod/tcp`'s own `configEndpoints`, carrying a
  198.51.100 address: TEST-NET-2 is global unicast and not private.

None of it was authorization. A node links to a gateway outside its swarm
without complaint — verified directly on loopback, where a `tcp` link to a
stranger gateway formed immediately.

## The loopback lab, which is how this was solved

Two bare astrald processes on 127.0.0.1, one configured as a gateway, iterate
in **seconds**. It reproduced the netsim failure exactly and then took it
apart; four of the seven netsim runs were spent on questions it answered at
once. The reason gateway is not simply an env `node` test is a harness gap:
`lib/nodeconfig.py` renders a fixed set of module configs and nothing lets a
test add one.

## Three requirements nothing writes down

Each cost a run to find, and each is the kind of thing the next person should
not have to rediscover:

- **Module YAML lives under `<root>/config/`,** not `<root>` —
  `cmd/astrald/run.go` joins `"config"` onto `-root`. A file one level up is
  read by nothing and the module keeps its defaults silently.
- **`nodes.new_link` takes `target`, not `id`.** A query missing a required
  argument is rejected by the decoder in microseconds, which reads exactly
  like a node refusing to link. Use the SDK binding.
- **Nothing teaches a lab node where a non-adopted node lives.** A node knows
  its swarm siblings from adoption and knows nothing else, so a gateway that
  was never adopted needs an explicit `nodes.add_endpoint` on every node that
  must reach it — and, for NAT'd clients, an address on their own network.

## What it does not cover

Unregistering. It is destructive and would have to run last, which would either
tear the link down before the oracle could read it or leave the decisive claim
resting on the driver's own account of what it saw. It earns a separate act
once this one is green.
