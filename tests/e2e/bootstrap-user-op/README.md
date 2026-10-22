# bootstrap-user-op

A node becomes a User node in one `user.bootstrap` call: node1 from the fixed
BIP-39 words `import-user-software-key` derives from, node2 from words it
generates under `-generate`.

- **env** node · **start** `null` · **saves** nothing
- **driver** `script.py` — as node1, `user.bootstrap` with the words, then a
  second `user.bootstrap` on the claimed node, then a token for the User. As
  node2, `user.bootstrap -generate`, then `bip137sig.seed`,
  `bip137sig.derive_key` and `crypto.public_key` on the words it returned.
- **oracle** `verify.py` — node1's first call answers the pinned
  `EXPECTED_USER_ID` and no words, its second is rejected with code 2, and
  node1 answers as that User with an active contract it is the subject of.
  node2's call answers 24 words that derive the identity it answered. Neither
  node's log holds any of the words.

**Saves nothing, and stays out of `main.suite`**, for the reasons
`import-user-software-key` states: a second producer of `one-node` would
shadow bootstrap's, and the env-node chain cannot return a node to unclaimed.

Run it alone: `./tests/run bootstrap-user-op`.

The pinned identity is the identity BIP-39 plus BIP-32 `m/1791'/0'/0'/0/0`
derives from the words. A pure-Python derivation outside astrald computed it;
`import-user-software-key` pins the same identity.
