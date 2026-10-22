# token-less-setup — a session without a token sets up a node and nothing else

A session without a token is confined to apphost's anonymous allowlist. Before
the claim it previews and claims the node, or joins a swarm from the new node;
after the claim it reaches `user.info` and `apphost.register` only.

- **env** node · **start** `null` · **saves** —
- **driver** `script.py` — on `claimer`, with no token: the step-by-step setup
  ops and `user.info`, then `user.derive_identity -generate`, then `user.bootstrap` with the returned words; authenticate with
  the token the claim returned; probe the claimed node with ops on and off the
  claimed list; register an app asking for `mod.auth.admin_manage_apps_action`
  and list tokens with it; ask `apphost.whoami` with the app token and no
  caller. On `joiner`, after both nodes learn each other's endpoint: with no
  token, `user.join -node <claimer>`, then `user.info` and a second
  `user.join`; with joiner's token, a third `user.join`. Record what came
  back.
- **oracle** `verify.py` — the unclaimed node refused every step-by-step op
  with `denied` and answered `user.info` with code 2, the previewed identity is
  the claimed one, the
  token authenticates as the User, the claimed node answers `user.info` and
  `apphost.register` and refuses the rest with `denied`, the registered app
  holds what it asked for, an empty caller from the app's session reads as the
  app, and `joiner` holds a contract from the same User, after which a
  token-less join is refused with `denied` and a join with joiner's token is
  rejected with code 2.

## What this proves that the unit tests do not

The unit tests hold each op and the gate with stand-ins. This drives a real
apphost session that presents no token through the gate to the ops, a real
token minted by `user.bootstrap`, and a real `user.join`: the request over a
link, the swarm node's join policy, and its `user.accept_membership` call back
into the joining node.

## Why its own nodes

The env-node chain cannot return node1 to unclaimed. `claimer` and `joiner`
are fresh daemons of their own, so the test starts at `null` and leaves the
chain's nodes untouched.

## Why the endpoints are added with node tokens

Loopback has no discovery, so each node learns the other's endpoint with
`nodes.add_endpoint`. That op is off the anonymous allowlist; the plumbing runs
with each node's token, and every step under test runs without one.
