# expel-authority

Who may call `user.expel`, what it refuses, and what a ban made twice does.

- **after** `adopt-node` (node1 the User's, node2 a member) ·
  **destroys** `true` — the test bans a node, and a ban cannot be lifted.
- **roster** node1, node2 and `ea-target`, a fresh node the User adopts so
  there is a member to expel and node2 stays in the swarm.
- **driver** `script.py` — the User adopts `ea-target`, then four callers
  expel, each recorded with node1's roster and bans read after it: a token
  minted for a fresh app identity on node1, node2's node token on node2, the
  User's identity on node2 (a token minted there; node2 holds no User key),
  and the User on node1 naming the User. Then `ea-target` is expelled twice:
  by node1's node token (the management node: its contract carries the admin
  permit with delegation, `NewNodeContract`), then by the User.
- **oracle** `verify.py` —
  - the two non-User callers that hold no admin permit read `not permitted`
    (`AuthorizeAdminSwarm`, `authorizers.go`: the contract's issuer, or a
    chain from it);
  - the User on a node without the key reads `no signing scheme available`:
    the node holds the contract, so the call is authorized and fails when
    `Expel` signs through crypto. The `sign as issuer` prefix, which the auth
    module adds elsewhere (`mod/auth/src/signing.go`), does not appear here;
  - the User as subject reads `cannot expel the swarm user` (`Expel`);
  - after each, node1's roster and ban list equal the baseline;
  - the duplicate answers cleanly: the second call returns a signed ban, and
    node1 stores exactly one, the first (`StoreExpulsion` is
    `OnConflict DoNothing`), so the stored `expelled_at` is the first
    call's; node2 receives the same single ban and drops `ea-target`;
  - the oracle repeats two refusals itself.

Not tested: expelling the node the caller is on (self-expel), an open
operator question.

Docs: `user.expel.md` omits `cannot expel the swarm user` and the key
requirement; this test pins both.
