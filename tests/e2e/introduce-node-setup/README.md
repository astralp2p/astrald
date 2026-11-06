# introduce-node-setup

A User's swarm loses the node that generated the User's words.

- **roster** `first`, `member`, `sibling`, `banned` — a world of its own
  (`stop` implies `destroys`).
- **driver** `script.py` — `first` claims a User with generated words;
  `member`, `sibling` and `banned` join it, and the User expels `banned`.
  The driver records the User, the words and `first`'s identity.
- **stop** the world stops `first` after the driver, for good.
- **oracle** `verify.py` — `first` is gone from the session view; `member`
  and `sibling` list each other and `first`, `first` unlinked, never
  `banned`, and each holds the ban.

`introduce-node` continues this world: it claims a replacement with the
same words and introduces it.
