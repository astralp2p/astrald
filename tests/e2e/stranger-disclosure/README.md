# stranger-disclosure — what a linked stranger reads of node1's User

A node of another User that is linked to node1 learns the User's identity two
ways, and the docs list both: `user.contract` answers the node's active
contract, and `dir.resolve localuser` names the User. It learns nothing of the
swarm's roster from either. A session with no link to node1 is not answered
`user.contract`.

- **env** node · **start** `adopt-node` · **saves** — (an observer: node1 and
  node2 end as adopt-node left them; the one change is the third node,
  `sdstranger`, claimed and linked)
- **driver** `script.py` — claim `sdstranger` for a second User with a
  token-less `user.bootstrap` and link it to node1 from its own side. As the
  stranger with its node token, over that link, call `user.contract` and `dir.resolve localuser` on node1 and record what
  each answered. As the User on node1, call `user.contract` locally.
- **oracle** `verify.py` — measured as the User on node1: the contract the
  stranger read has node1's active contract's object id, issuer (the User),
  subject (node1), permits and expiry (about 365 days out); `localuser`
  resolved to the same User identity; neither answer holds node2's identity or
  the stranger's; the User's own session was answered `not permitted`. The
  stranger is linked to node1 and claimed by another User.

## The documented exposure

astral-docs `topics/node-exposure.md`, "Linked peers": a linked peer with no
grant reaches `user.contract` and `dir.resolve`; "the contract names the
user. Its permits show whether the node is a management node. Its expiry …
shows when the user claimed or adopted the node." `protocols/user/ops/
user.contract.md` repeats it under "Disclosure" and names the one guard: a
caller with no direct link is answered `not permitted`, and a User token calls
as the User, which holds no link. `protocols/dir/ops/dir.resolve.md` lists
`localuser` among the names a resolver claims. The source agrees:
`mod/user/src/op_contract.go` checks `Nodes.IsLinked(q.Caller())` and nothing
else, `mod/dir/src/op_resolve.go` has no guard, and
`mod/user/src/identity_resolver.go` resolves `localuser` for any caller.

This test asserts that exposure as it is, so a change that narrows it (a grant
required for `user.contract`) or widens it (an answer to an unlinked caller,
more in the answer) fails here and is a decision to make, not a quiet drift.
What it means for Stealth: nearby hides the User from the LAN, but the first
linked node reads it.

## Why not the roster probes again

`swarm-authz-matrix` already probes `user.info`, `user.swarm_status` and
`user.list_siblings` as its stranger and asserts each is refused, so those
rows are not repeated. This test adds the two names that test's design lists
as not covered, and asserts the roster is absent from what the stranger does
read.

## Why a stranger of its own, and not `after = "swarm-authz-matrix"`

Continuing the matrix would reuse its stranger, but `after` turns a test that
nothing continues (an observer, which shares the world) into one that
something continues, and the planner then forks the shared world: the matrix
leaves `w0` and a new world holds it and this test. `selftest/test_plan.py`
holds `main.suite` to one shared world and one world per destroyer, and fails
with that fork. As a sibling under `adopt-node` this test stays an observer,
runs after the matrix in suite order, and builds the same fixture itself: a
node claimed by a second User through a token-less bootstrap, linked to node1
from its own side. The name `sdstranger` is its own, so the order of the two
tests does not matter and either runs alone.

The stranger stays linked, as the matrix's does: no later test in the world
asserts an exact link set or an exact provider set.
