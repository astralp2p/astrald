# app-origin-guards — a swarm sibling cannot register, host or discover on its peer

The User reaches `apphost.register`, `apphost.register_handler` and
`services.discover reach=swarm` on node1. A swarm sibling relaying as that same
User does not.

- **env** node · **start** `two-nodes` · **saves** —
- **driver** `script.py` — mint a User token on node2, then send node1 the
  three ops through that session (`target=node1`, no `for=` on the discovery).
  Record what came back, and what the same three ops do for the User locally on
  node1 in the same run, and how many tokens node1 holds before and after.
- **oracle** `verify.py` — the control calls answered, each probe was refused
  and reads as its guard's refusal, and node1 holds the same tokens it held
  before the probes.

## What this proves that the unit tests do not

`apphost.register` and `apphost.register_handler` answer a query off a link
with `origin not allowed: network`; `services.discover` answers
`reach=swarm` off a link with an error that says an app asks only its own
node. The User identity satisfies every authorization check these ops make,
and `mod/user` authorizes `RelayForAction` for a swarm node relaying on behalf
of the User, so node2 sends a relay query naming the User as its caller and
node1 admits it. The origin check is the only thing left between a sibling and
a provisioned identity, a handler claiming the User's queries, or a swarm-wide
discovery run in the User's name.

The unit tests hold each half: an op refuses a query stamped with network
origin. Neither runs the path between them — a real link, a real relay,
`mod/nodes` stamping the origin, and `core.Router` carrying it to the op. This
drives that path.

## Scope

`apphost-origin` already covers `apphost.create_token` and `apphost.list_tokens`,
and `services-advertise-identity` covers `services.advertise` over a link. This
test covers the three ops neither reaches.

`services.discover` refuses differently from the two apphost ops, and the
oracle expects each op's own text. The discovery refusal is an `error_message`
reading `reach=swarm is accepted only from a local app`; only its head is
matched. A discovery relayed over a link without `reach=swarm` is not refused:
the node answers from its own providers.

Since provider visibility (#414), a discovery caller without a permit is no
longer refused; it sees nothing. The control discovery therefore names a
service nobody advertises and asks only that the node run it to its end.

## Why the control calls matter

A refusal and a broken op leave the caller holding the same nothing. The driver
records each op answering the User on node1, in this same run and on the same
node, and the oracle checks those first. Without them the test would stay green
against a node whose apphost or services module was down.

The oracle also requires each failure to *read* as that op's refusal. A
timeout, a lost route or an unreachable node would otherwise keep this green
with the guards gone.

## Why the sibling mints its own User token

`user_token` in the session facts is node1's apphost token and authenticates
nowhere else, so it cannot open a User session on node2. node2's own token holds
`AdminManageApps` on node2 as this node, and may mint a User token there. That
token makes the relayed query carry the User as its caller. The token stays on
node2, as `apphost-origin`'s does.

## Why the oracle lists node1's tokens itself

Whether `apphost.register` was refused and whether an identity was nevertheless
provisioned are two claims, and the driver is the wrong party for the second.
Provisioning always creates an access token, so the oracle lists node1's tokens
and requires the count the driver took before the probes, and the control's
identity among them.

## Limits

No op lists a node's registered handlers, so a handler registered by a missing
guard is seen only as the op answering. The control's handler is removed by
closing its `apphost.bind` session; it points at a dead endpoint and is never
called.
