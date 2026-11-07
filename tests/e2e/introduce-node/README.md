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
  and each holds the ban. Both user.info answers carry the contract of the
  node asked, signed by the User. The token-less query is denied by the
  gate, the joined node's token is not permitted, naming the node itself
  answers `cannot introduce to self`, and the second introduction answers
  `ack`.

`sibling` lists `replacement` only after `replacement` linked to it and
pushed its contract: `member` forwards no contract it receives, so the
oracle's view of `sibling` proves the sync beyond the introduced node.
