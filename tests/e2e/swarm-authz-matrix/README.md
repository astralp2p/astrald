# swarm-authz-matrix — what a linked stranger and a Member sibling may do to node1

**Since G7 the sibling row is a Member's.** node2 is a `privileged_member`,
which acts for the User in every check but an expel (Swarm State and User
Authority, D6), so the any-member read row G7 removed is gone. A Member,
`authz_member`, now holds the narrowed rules: it reads the roster
(SeeSwarm admits every member) and nothing else. The oracle still reads the
bait as node2.

A node the User does not own links to node1 and reaches only the ops every
linked peer reaches. Its own User reaches nothing. A Member sibling reaches
the swarm's roster reads and none of node1's objects, links or writes. node1
holds nothing any of them tried to write.

- **env** node · **start** `two-nodes` · **destroys** — `authz_member` joins
  the User's swarm, and the tests sharing adopt-node's world compare exact
  rosters, so this test runs in a world of its own
- **driver** `script.py` — as the User, store a bait object on node1. Claim
  `stranger` for a second User with a token-less `user.bootstrap`, and link it
  to node1 from the stranger's side. `authz_member` joins through node1 as a
  Member, node1 signing its membership in the carrier session
  (`swarmstate.admit`). Send 21
  probes to node1 over the link (`target=node1`) as three askers: the
  stranger with its node token, the stranger with its User's token, and the
  Member with its node token. Record each outcome as `allowed`, `refused` or
  `reset`.
- **oracle** `verify.py` — each asker's allowed set is exactly the contract
  below, and every other probe reads as a refusal. node1's state, measured as
  the User: the bait is present and hashes to its id; the roster is node1,
  node2 and the Member; no expulsion exists; the alias `hijack`, the tree path and the
  intruder object do not exist; no token names the stranger. Probes of its
  own: the stranger and the Member are refused the bait, node2 reads it, and
  node1 sees the stranger and the Member as themselves.

## The contract

| asker | node1 answers |
|-------|---------------|
| stranger, node token | `apphost.whoami`, `dir.resolve`, `shell.spec` |
| stranger, its User's token | nothing, the three above included |
| authz_member (a Member), node token | the three above, `user.info`, `user.swarm_status`, `user.list_siblings` |

The stranger's row is astral-docs `topics/node-exposure.md`, "Linked peers": a
peer with no grant reaches the listed ops and is refused everything else. The
second row holds because node1 admits a relay only from a swarm node relaying
for node1's own User (`AuthorizeRelayFor`), so the query is rejected before an
op sees it. The Member's row is the narrowed rules: SeeSwarm admits every
member; SeeObjects grants the User and node1, and a member only through its
contract chain (`mod/user/src/authorize_by_chain.go`), which a Member's
membership, carrying only its tier permit, does not give; AdminNetwork grants
the User and node1 only, and no member of any tier
(`authorize_admin_network.go`); StoreObjects, AdminObjects and AdminSwarm
grant the User and node1 only, and the apphost setters refuse a network
origin. Before G7 a sibling of any tier also read node1's objects and links.

The guarded probes are `tree.list`, `apphost.list_tokens`, `objects.store`,
`tree.set`, `dir.set_alias`, `apphost.create_token`, `apphost.hold_object`,
`user.expel`, `crypto.sign_text` and `objects.delete` (G7 removed the
`user.adopt` and policy-setter probes with their ops).
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
node1 as the User for every write a probe attempted.

## Why the stranger stays linked

No test after this one in its world asserts an exact link set or an exact
provider set: read-swarm asks only that node2 is among an object's providers,
and the stranger holds none of their objects. The oracle needs the link for its
own probes. The stranger dials node1, so node1 is given no endpoint for it.
