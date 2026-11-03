# introduce-node

A node claimed with the User's words joins the User's running swarm through
`user.introduce`.

- **roster** `first`, `member`, `sibling`, `banned`, `replacement` — a world
  of its own (`destroys`).
- **driver** `script.py` — `first` claims a User with generated words;
  `member`, `sibling` and `banned` join it, and the User expels `banned`. A
  manager claims the fresh `replacement` with the same words, and
  `replacement` knows `member`'s endpoint alone. As the User, `replacement`
  introduces itself to `member`. Probes: the op without a token, from
  `sibling`'s node token, naming `replacement` itself, and again with
  `replacement`'s node token.
- **oracle** `verify.py` — before the introduction no member lists
  `replacement` and `replacement` lists itself alone; afterwards `member`,
  `sibling` and `replacement` list each other, never `banned`, with the
  aliases each gives itself, and each holds the ban. The token-less query is
  denied by the gate, the joined node's token is not permitted, naming the
  node itself answers `cannot introduce to self`, and the second introduction
  answers `ack`.

`sibling` lists `replacement` only after `replacement` linked to it and
pushed its contract: `member` forwards no contract it receives, so the
oracle's view of `sibling` proves the sync beyond the introduced node.

`first`, the node that generated the words, stays up: a driver cannot stop a
node, and a node that dies under a test fails it. `replacement` still starts
alone, since no member holds its contract.
