# manager-setup — a fresh node appoints one manager, and the manager claims it

`sys.register_manager` appoints the manager of a fresh node. The manager
claims the node with its App token, and the claim keeps the appointment. An
app registered later is no manager.

- **env** node · **start** `null` · **saves** —
- **driver** `script.py` — read `user.info` on the fresh node, appoint a
  manager from a session with the node's token, ask for a second appointment, read
  `user.info`, ask as the manager for an op outside the `unclaimed` gate list,
  serve an op as the manager on the `unclaimed` node and query it with the node's
  token, register an ordinary app and ask it to claim, claim as the manager with
  `user.bootstrap`, then read `user.info` with the User token and ask for a
  third appointment.
- **oracle** `verify.py` — the node read `fresh` with no manager, then
  `unclaimed` naming the manager, then `claimed` naming the same manager; the
  gate denied the second appointment on the `unclaimed` node, and the
  appointment on the `claimed` node answered `node is not fresh`; the gate denied the
  manager's token query outside the `unclaimed` list; the manager's handler
  registered on the `unclaimed` node and answered the node's identity; the ordinary app was not
  permitted to claim; the manager's claim answered a User and a token.

## What this proves that the unit tests do not

The unit tests hold the appointment, the gate and the claim ops apart, each
with stand-in modules. This drives them through one real node: the tree
records the manager, the real user module reads it to admit the claim, and the
apphost gate reads the state the two modules produce.

## Why the E2E gate

The harness writes the E2E overrides (`tests/lib/nodeconfig.py`): `fresh`
admits every op and the policies accept. The `unclaimed` list keeps its
default, so the gate check on the manager's token query is the distribution's.
The E2E `tokenless` list omits `sys.register_manager`, so the appointment
runs with the node's token.
