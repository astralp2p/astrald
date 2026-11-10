Set up the astral node `{node1.alias}` (identity `{node1.identity}`), the node
your `astral-query` reaches. It is new and belongs to nobody. A seed phrase
and a passphrase together name one User, so the passphrase must travel with
the words every time they are used:

```
horse soldier imitate stool square buyer verb party enjoy result jazz rabbit trigger file benefit cloth term change
```

The passphrase is `astral-passphrase`.

Before the node is claimed, find the identity those words derive, along the
user key path `m/1791'/0'/0'/0/0`, in four ways, and keep each identity:

- with the node's identity preview for a User (`user.derive_identity`), with
  the passphrase (`derive_with`) and without it (`derive_without`);
- by turning the words into a seed (`bip137sig.seed`), deriving the key at that
  path from the seed and reading the key's public identity, with the passphrase
  (`seed_with`) and without it (`seed_without`).

Then claim the node for the User these words derive with the passphrase, using
the node's own claim op (`user.bootstrap`), and keep the User's id (`user_id`)
and an access token for the User (`user_token`).
