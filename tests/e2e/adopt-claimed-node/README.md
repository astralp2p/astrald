# adopt-claimed-node

A node that already belongs to a User refuses another User's adoption, and
stays where it was.

- **after** `adopt-claimed-node-setup` (User B owns `acn-b1` and `acn-b2`,
  beside the first User's node1 and node2) · **no destroys** — a refused
  adoption changes no node's state, and `acn-b1` and `acn-b2` are this pair's
  own nodes that no other test names, so the shared world keeps serving its
  siblings. The pair continues `main.suite`'s shared world after
  `object-store-peer`'s observers; only one non-destroying test per parent
  continues a world, and `adopt-node`'s is `object-store-peer`
  (`selftest/test_plan.py`, one world per destroyer).
- **roster** `node1`, `acn-b1`, `acn-b2`.
- **driver** `script.py` — node1 learns `acn-b2`'s endpoint and links to it
  (so the node can be reached and the answer is its own); as the first User
  the driver asks `user.adopt` for `acn-b2`. The answer's text is the fact
  `acn_adopt_outcome`.
- **oracle** `verify.py` — the adoption was refused, as `node already claimed`
  or `session closed` (below); `acn-b2` is still `claimed` under B, holds the
  contract id the setup recorded, and lists `acn-b1`, never node1; User B's
  roster on `acn-b1` still lists `acn-b2`; the first User's roster on node1
  does not.
- **prompt** `prompt.md` — the operator, as the first User, is asked to bring
  `acn-b2` into its swarm and to keep astral's answer. The oracle judges
  state, so the operator's words only need to name the refusal.

## Two refusal messages

`OpAcceptMembership` (`mod/user/src/op_accept_membership.go:34-38`) answers
`user.ErrAlreadyClaimed` ("node already claimed") before it reads the contract,
and closes. The inviter's client sends the contract first
(astral-go `api/user/client/accept_membership.go:20-23`) and returns the send
error when the node has already closed, so `user.adopt` often answers `session
closed` instead of the node's reason. Measured on 20 attempts against one
claimed node: 3 `node already claimed`, 17 `session closed`, the same through
astral-py and the JSON op. The oracle accepts either; an adoption that
succeeds or fails otherwise (unreachable, not permitted) is neither. The
node's refusal is real in both; the reason reaching the caller only some of
the time is a defect of the invitation handshake, not asserted here.
