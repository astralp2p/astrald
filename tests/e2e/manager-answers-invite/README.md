# manager-answers-invite — an unclaimed node's manager answers the swarm invite

An unclaimed node has no owner to say whether it may join a swarm, so its
manager does: `sys.register_manager` binds the `swarm_invite` policy to an op
the manager serves, and the node asks that op about every invitation. A manager
that approves brings its node into the inviting User's swarm; one that declines
leaves it unclaimed. The manager answers as an app, so it must bind and attach
a handler before the claim, which is why `apphost.bind` and
`apphost.register_handler` (and `apphost.cancel`, see below) are on the shipped
`unclaimed` gate list.

- **env** node · **start** `null` · **saves** —
- **roster** `mai-inviter` (claimed by the driver, E2E gate), `mai-approve` and
  `mai-decline` (fresh, shipped gate)
- **driver** `script.py` — claim `mai-inviter` with a session that has no
  token, link it to both invitees with the User's token, then run `driver/`
  (Go) once per invitee: with the node's token appoint a manager whose
  `swarm_invite` delegate is the op `mai.decide`, serve that op as the manager,
  ping the manager as the node to show the handler routable (a failure to serve is recorded, not fatal), read `user.info`,
  and have the User on `mai-inviter` call `user.adopt` for the invitee. The
  manager of `mai-approve` answers allow, the manager of `mai-decline` answers
  deny. The driver records what each manager was asked and how each adoption
  ended.
- **oracle** `verify.py` — the two nodes appointed different managers; before
  the invitation each read `unclaimed` naming its manager; each manager was
  asked exactly once, by `mai-inviter`'s identity, for a contract issued by the
  User to the invited node; the approved adoption returned a contract for
  `mai-approve`, which now reads `claimed` under the User and still names its
  manager; the declined adoption answered `invitation declined`, and
  `mai-decline` reads `unclaimed` with no contract, still naming its manager;
  the inviter's swarm lists `mai-approve` and not `mai-decline`.

## What this proves that the unit tests do not

`swarm_policy_test.go` and `op_set_swarm_policy_test.go` hold the policy
resolution and the setter with stand-ins, and `config_gate_test.go` holds the
gate's lists against a file. This runs the whole path through real nodes: the
appointment stores the delegate, the manager registers its handler through a
gate that admits only the unclaimed list, a remote `user.adopt` reaches
`user.accept_membership`, the node routes the question to the manager's app,
and the manager's answer decides whether the node signs.

## Why the shipped gate

The harness's E2E gate opens a `fresh` node to every op, so a node under it
would let the manager bind whatever the list says. `test.toml` gives the two
invitees `gate = {}` and `policies = {}`, as `distribution-gate-setup` does, so
they start under the lists in `mod/apphost/src/config.go` (`defaultConfig`). An
`unclaimed` list without `apphost.bind`, `apphost.register_handler` or
`apphost.cancel` leaves the manager unable to bind or attach its handler, and the
oracle reads the failure. `apphost.cancel` is on the list too, and this flow
never calls it: dropping it leaves the test green. `mai-inviter` keeps the E2E gate, which claims it with no token.

## Why its own nodes, and no `destroys`

The invitation claims `mai-approve`, and the env-node chain cannot return a node
to unclaimed, so the three nodes are this test's own, with names no other test
uses, as `token-less-setup` does. No other test follows it in a world, so it
destroys nothing.

## Why a Go driver

astral-py has no codec for `mod.user.swarm_invite_request` or
`mod.user.swarm_invite_decision`, and a manager answers them as an app.

## Why the inviter alone learns an endpoint

The `unclaimed` list holds no `nodes.add_endpoint`, so an invitee cannot be
told the inviter's endpoint. The inviter links to the invitee, and the
invitation and its answer travel over that link.
