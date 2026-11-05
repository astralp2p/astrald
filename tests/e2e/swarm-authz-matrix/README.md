# swarm-authz-matrix — what a linked stranger and a sibling may do to node1

A node the User does not own links to node1 and reaches only the ops every
linked peer reaches. Its own User reaches nothing. A swarm sibling reaches the
swarm's reads and none of node1's writes. node1 holds nothing any of them tried
to write.

- **env** node · **start** `two-nodes` · **saves** — (an observer: node1 and
  node2 end as adopt-node left them, plus one bait object)
- **driver** `script.py` — as the User, store a bait object on node1. Claim
  `stranger` for a second User with a token-less `user.bootstrap`, and link it
  to node1 from the stranger's side. Send 24 probes to node1 over the link
  (`target=node1`) as three askers: the stranger with its node token, the
  stranger with its User's token, and node2 with its node token. Record each
  outcome as `allowed`, `refused` or `reset`.
- **oracle** `verify.py` — each asker's allowed set is exactly the contract
  below, and every other probe reads as a refusal. node1's state, measured as
  the User: the bait is present and hashes to its id; the roster is node1 and
  node2; no expulsion exists; the alias `hijack`, the tree path and the
  intruder object do not exist; no token names the stranger; no policy
  delegate names it. Three probes of its own: the stranger is refused the bait,
  node2 reads it, and node1 sees the stranger as itself.

## The contract

| asker | node1 answers |
|-------|---------------|
| stranger, node token | `apphost.whoami`, `dir.resolve`, `shell.spec` |
| stranger, its User's token | nothing, the three above included |
| node2, node token | the three above, `user.info`, `user.swarm_status`, `user.list_siblings`, `objects.read`, `objects.contains`, `objects.load`, `objects.repositories`, `nodes.links` |

The stranger's row is astral-docs `topics/node-exposure.md`, "Linked peers": a
peer with no grant reaches the listed ops and is refused everything else. The
second row holds because node1 admits a relay only from a swarm node relaying
for node1's own User (`AuthorizeRelayFor`), so the query is rejected before an
op sees it. The sibling's row is `mod/user/src/authorizers.go`: SeeSwarm,
SeeObjects and AdminNetwork grant the local swarm; StoreObjects, AdminObjects
and AdminSwarm grant the User and node1 only, and the apphost and policy
setters refuse a network origin.

The guarded probes are `tree.list`, `apphost.list_tokens`, `objects.store`,
`tree.set`, `dir.set_alias`, `apphost.create_token`, `apphost.hold_object`,
`user.adopt`, `user.expel`, `user.set_swarm_join_policy`,
`apphost.set_app_register_policy`, `crypto.sign_text` and `objects.delete`.
Every write names the stranger or the bait, so a guard that broke changes only
what the oracle reads back.

## Why the allowed set must be exact

A grant beyond the contract is the defect this test exists to catch. A grant
missing from it is a broken swarm, and it also makes the refusals say nothing:
a node that answers nobody refuses every probe. The positive controls
(`apphost.whoami` naming the asker, `dir.resolve` naming node1) show each
probe reached node1 and that node1 saw the asker it was meant to see.

## Why a reset is judged by state

`objects.store`, `tree.set` and `crypto.sign_text` carry a body. Refused over a
link, they do not read as `not permitted`: node1 refuses before it reads the
body, the relay resets the stream on the body already sent, and the refusal is
lost. Which way it is lost is a race. The caller sees
`ConnectionResetError: Connection lost`, or a `ProtocolError` such as
`crypto.sign_text: … destroyed its own error message`,
`objects.store: the stream ended after 0 of 1 answers`, or
`tree.set: sent 1 object(s) and got 0 answer(s)`. The same ops answer
`not permitted` to a local caller. A reset reads like a fault, so the oracle
accepts it for these three probes only, never reads its text, and decides them
by node1's state: no intruder object and no tree value. This shape looks
like an astrald defect and is filed separately.

## Why the oracle measures node1 itself

The driver reports refusals; whether anything was written regardless is a
separate claim, and the driver is the wrong party to make it. The oracle reads
node1 as the User for every write a probe attempted. It checks the policy
delegates for one naming the stranger rather than for none, because
policy-delegate runs earlier in the shared world and may leave its own
`app_register` delegate.

## Why the stranger stays linked

No test after this one in its world asserts an exact link set or an exact
provider set: read-swarm asks only that node2 is among an object's providers,
and the stranger holds none of their objects. The oracle needs the link for its
own probes. The stranger dials node1, so node1 is given no endpoint for it.
