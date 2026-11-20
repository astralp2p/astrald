# expel-management-node

An expelled management node holds a valid membership contract and still gets
every swarm call refused.

- **after** none · **destroys** `true` — the test bans a node, and a ban
  cannot be lifted. It runs in a world of its own, over three fresh nodes.
- **roster** `emn1`, `emn2`, `emn3`. The driver makes one User from new words
  and claims `emn1` and `emn2` for it with `user.bootstrap`, so each holds a
  management contract (admin and see permits with delegation 1,
  `NewNodeContract`). `emn2` offers its contract to `emn1`, which indexes it.
  The User on `emn1` adopts `emn3`, a plain member.
- **driver** `script.py` — with a token `emn1` mints for `emn2`'s identity, the
  controls (`user.list_expelled`, `user.list_siblings`, `user.expel` naming the
  User); then the User on `emn1` expels `emn2`; then the same three calls and
  `user.expel` naming `emn3`, with `emn1`'s roster and bans read after each.
- **oracle** `verify.py` — the controls are answered, and `user.expel` meets
  `cannot expel the swarm user`, which only an authorized caller reaches; after
  the ban each call reads `not permitted`; `emn1`'s roster is `emn1` and
  `emn3` and its one ban names `emn2` after every call; the oracle repeats the
  calls itself.

Why a local token and not `emn2`'s own link: the token is `emn2` as far as
`emn1`'s authorizers can tell, and no ban answer from `emn2` can close a link
under the calls. A direct call from `emn2` would race that answer.

Without the check in `authorize` the three reads and the expel are allowed:
`AuthorizeSeeSwarm` and `AuthorizeAdminSwarm` reach the User through the
contract `emn2` still holds (`mod/user/src/authorizers.go`), and the expulsion
list is not read. The user module registers `isBanned` as a veto the auth
module asks at every level of a walk (`mod/auth/src/authorize.go`), so the
contract and any chain through an expelled issuer give nothing.

Not tested: paths that never call `Authorize` while the expelled node is still
linked (`dir.resolve`, `user.contract`, a hosted app); that is a separate
ticket.
