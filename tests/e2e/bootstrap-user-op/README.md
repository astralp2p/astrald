# bootstrap-user-op

A node becomes a User node in one `user.bootstrap` call: node1 from the fixed
BIP-39 words `import-user-software-key` derives from, node2 from words it
generates under `-generate`.

- **env** node · **start** `null` · **saves** nothing
- **driver** `script.py` — as node1, `user.bootstrap` with the words, then a
  second `user.bootstrap` on the claimed node, made again while its
  connection is reset, keeping the User's token the first call returned. As
  node2, `user.bootstrap -generate`, then `bip137sig.seed`,
  `bip137sig.derive_key` and `crypto.public_key` on the words it returned.
- **oracle** `verify.py` — node1's first call answers the pinned
  `EXPECTED_USER_ID`, no words and a token, its second answers only the
  `error_message` `node already claimed`, and node1 answers that token as the
  User with an active contract node1 is the subject of. node2's call answers
  24 words that derive the identity it answered, and a token. Neither node's
  log holds any of the words.

**Destroys its world.** It claims node1, as `bootstrap-user-software-key`
does, so its manifest sets `destroys = true` and the planner gives it a world
of its own. `main.suite` lists it.

Run it alone: `./tests/run bootstrap-user-op`.

The pinned identity is the identity BIP-39 plus BIP-32 `m/1791'/0'/0'/0/0`
derives from the words. A pure-Python derivation outside astrald computed it;
`import-user-software-key` pins the same identity.
