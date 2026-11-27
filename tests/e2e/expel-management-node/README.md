# expel-management-node

An expelled Privileged node holds a valid membership and still gets every
swarm call refused.

- **after** none · **destroys** `true` — the test bans a node, and a ban
  cannot be lifted. It runs in a world of its own, over three fresh nodes.
- **roster** `emn1`, `emn2`, `emn3`. The driver makes one User from new words
  and claims `emn1` and `emn2` for it with `user.bootstrap`, so each holds a
  Privileged membership the user key signed. `emn2` offers its membership to
  `emn1`'s SwarmState with `user.swarm_push`. `emn3` joins through `emn1` as a
  Member, `emn1` signing its membership in the carrier session
  (`swarmstate.admit`).
- **driver** `script.py` — with a token `emn1` mints for `emn2`'s identity, the
  controls (`user.list_expelled`, `user.list_siblings`, `user.expel` naming the
  User); then the User on `emn1` expels `emn2`; then the same three calls and
  `user.expel` naming `emn3`, with `emn1`'s roster and bans read after each.
- **oracle** `verify.py` — the two read controls are answered, and
  `user.expel` reads `not permitted` even before the ban, since only the user
  expels (D7); after the ban each call reads `not permitted`; `emn1`'s roster
  is `emn1` and `emn3` and its one ban names `emn2` after every call; the
  oracle repeats the calls itself.

G7 rewrote this test. The "management contract" (admin and see permits with
delegation 1) and its push with `objects.push` are gone: a bootstrapped node
holds a Privileged membership, and memberships travel through SwarmState. The
expel control changed with D7: before, a management node reached `user.expel`
and met "cannot expel the swarm user"; now no node but the user reaches it.

Why a local token and not `emn2`'s own link: the token is `emn2` as far as
`emn1`'s authorizers can tell, and no ban answer from `emn2` can close a link
under the calls. A direct call from `emn2` would race that answer.

Without the ban the two reads are allowed: SeeSwarm admits every member, and
a Privileged membership administers the swarm locally. The user module registers `IsBanned` as a veto the auth
module asks at every level of a walk (`mod/auth/src/authorize.go`), so the
contract and any chain through an expelled issuer give nothing.

Not tested: paths that never call `Authorize` while the expelled node is still
linked (`dir.resolve`, `user.contract`, a hosted app); that is a separate
ticket.
