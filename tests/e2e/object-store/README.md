# object-store

Story 0006. node1, acting as the User, stores a fixed payload in its own
repository and the oracle reads it back.

- **env** node · **start** `two-nodes` · **saves** `two-nodes-data`
- **driver** `script.py` — `objects.create` + `write` + `commit` on node1.
- **oracle** `verify.py` — repo-pinned `objects.load` on node1 must return
  `payload.txt` byte for byte.

`payload.txt` is the ground truth. The driver never gets to say what it
stored: the oracle compares the repository's bytes against the file the
driver was given.

Raw bytes go through `create` + `write` + `commit`, not `objects.store` —
`store` canonicalizes a typed object and refuses an untyped blob with
`empty type`.
