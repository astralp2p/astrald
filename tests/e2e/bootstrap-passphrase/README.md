# bootstrap-passphrase — the same words and passphrase name one User on every path

A seed phrase with a passphrase names a different User than the same phrase
without one. A path that drops the passphrase silently yields another User, and
nothing fails: the node claims, the contract signs, the key is valid. This test
holds the three paths that turn words into a User to one pinned identity.

- **env** node · **start** `null` · **saves** — · **destroys** its world: it
  claims node1
- **driver** `script.py` — on fresh node1, with the pinned words and the
  passphrase `astral-passphrase` as the `passphrase` query argument:
  `user.derive_identity`; `bip137sig.seed`, `bip137sig.derive_key` at
  `m/1791'/0'/0'/0/0` and `crypto.public_key`; each again with no passphrase;
  then `user.bootstrap` with the passphrase, which claims node1 and returns the
  User's token.
- **oracle** `verify.py` — `derive_with`, `seed_with` and the identity
  `user.bootstrap` answered equal the pinned `WITH_PASSPHRASE`; `derive_without`
  and `seed_without` equal the pinned `WITHOUT_PASSPHRASE`, which differs; the
  token authenticates as `WITH_PASSPHRASE`, and `user.info` names it as the
  contract's issuer and node1 as its subject, so the claim stored the key the
  passphrase derives and not the one the bare words derive.
- **drivers** both: `prompt.md` asks an operator for the same four derivations
  and the claim. It is not in `agent.suite`.

## Pinned identities

Both are BIP-39 plus BIP-32 `m/1791'/0'/0'/0/0` over the words
`horse soldier imitate … change` (the words `bootstrap-user-op` and
`import-user-software-key` use), computed outside astrald in plain Python:
PBKDF2-HMAC-SHA512 with the salt `mnemonic` plus the passphrase, BIP-32 child
derivation, and a secp256k1 point multiplication. The derivation reproduces
the `02564b…` identity the two earlier tests pin for the empty passphrase;
`02f1d2…` is the same derivation with `astral-passphrase`.

| passphrase | identity |
|------------|----------|
| none | `02564ba75b0fc5869e12c0c89e56a9c4fbec9d23ed01e6d240aac80532fcb10af1` |
| `astral-passphrase` | `02f1d2c29346ca6d1394ed15e725f8e8449c3f75fa77d8b484a50ae4b000637ce2` |

## Why node1, and a world of its own

It claims a fresh node, so the manifest sets `destroys = true` and the planner
gives it a world of its own, as for `bootstrap-user-op`. The roster is node1
alone because an operator reaches node1 only (`lib/agent.py`). The ops that
only read run before the claim, on the fresh node under the harness's E2E gate.

Run it alone: `./tests/run bootstrap-passphrase`.
