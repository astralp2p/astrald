# apphost-origin — a swarm sibling cannot mint a token on its peer

The User reaches `apphost.create_token` and `apphost.list_tokens` on node1. A
swarm sibling relaying as that same User does not.

- **env** node · **start** `two-nodes` · **saves** —
- **driver** `script.py` — mint a User token on node2, then ask node1 for a
  token and for its token list through that session. Record what came back, and
  what the same two ops do for the User locally on node1 in the same run.
- **oracle** `verify.py` — the control calls answered, both probes were refused,
  each refusal reads as a refusal, and node1 holds no token for the identity the
  probe named.

## What this proves that the unit tests do not

The token ops answer to
`mod.auth.admin_manage_apps_action`, and the User identity holds it. That check
alone does not keep the ops off the network. `mod/user` authorizes
`RelayForAction` for a swarm node relaying on behalf of the User, so node2 sends
a relay query naming the User as its caller, node1 admits the relay, and the op
sees the User — an identity that satisfies the authorization check. The origin
refusal is the only thing left between a sibling and a minted token.

The unit tests hold each half: that an op refuses a query stamped with network
origin, and that the authority is asked for a local one. Neither runs the path
between them — a real link, a real relay, `mod/nodes` stamping the origin, and
`core.Router` carrying it to the op. This drives that path.

## Why the control calls matter

A refusal and a broken op leave the caller holding the same nothing. The driver
records `apphost.create_token` and `apphost.list_tokens` answering the User on
node1, in this same run and on the same node, and the oracle checks those first.
Without them the test would stay green against a node whose apphost was down.

The oracle also requires each failure to *read* as a refusal. A timeout, a lost
route or an unreachable node would otherwise keep this green with the guard
gone.

## Why the sibling mints its own User token

`user_token` in the session facts is node1's apphost token and authenticates
nowhere else, so it cannot open a User session on node2. node2's own token
authenticates as node2, which holds `AdminManageApps` on node2 as this node, and
may therefore mint a User token there. That token is what makes the relayed
query carry the User as its caller.

## Why the oracle lists node1's tokens itself

The driver reports whether it was refused; whether anything was nevertheless
minted is a separate claim, and one the driver is the wrong party to make. The
oracle asks node1 directly for the tokens it holds for the probed identity. A
refusal that still left a token behind would otherwise read as a pass.
