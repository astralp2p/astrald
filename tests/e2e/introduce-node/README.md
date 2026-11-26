# introduce-node

A node claimed with the User's words joins the User's swarm through
`user.introduce`, after the node that generated the words is gone.

- **world** continues `introduce-node-setup`: `member`, `sibling` and
  `banned` joined `first`, the User expelled `banned`, and the world stopped
  `first`.
- **roster** `member`, `sibling`, `banned`, `replacement`.
- **driver** `script.py` — a manager claims the fresh `replacement` with the
  words `introduce-node-setup` recorded, and `replacement` knows `member`'s
  endpoint alone. As the User, `replacement` introduces itself to `member`.
  Probes: the op without a token, from `sibling`'s node token, naming
  `replacement` itself, and again with `replacement`'s node token. Last,
  user.info as the User across the link: from `replacement` of `member`,
  and from `member` of `replacement` with a User token `member` mints.
- **oracle** `verify.py` — `first` is not in the session view; before the
  introduction no member lists `replacement` and `replacement` lists itself
  alone; afterwards `member`, `sibling` and `replacement` list each other,
  never `banned`, with the aliases each gives itself, list `first` unlinked,
  and each holds the ban. user.info relayed by `replacement` (Privileged)
  carries `member`'s membership, signed by its carrier `first`; relayed by
  `member` (a Member) it is refused, since a Member does not relay as the
  User (D5, D6). The token-less query is denied by the
  gate, the joined node's token is not permitted, naming the node itself
  answers `cannot introduce to self`, and the second introduction answers
  `ack`.

`user.introduce` offers `replacement`'s Privileged membership to `member`'s
SwarmState (`nodes.swarm_push`) and pulls `member`'s; `member` passes the
stored entry on to its peers, so `sibling` lists `replacement` through
SwarmState. Before G7 the op pushed the contract with `objects.push`, which
the new push admission rejects.
