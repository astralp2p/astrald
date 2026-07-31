# import-user-software-key

Story 0002. node1 becomes a User node from an **existing** BIP-39 mnemonic —
the alternative path to `bootstrap-user-software-key`, which mints a fresh
key.

- **env** node · **start** `null` · **saves** nothing
- **driver** `script.py` — `bip137sig.seed` on the fixed mnemonic, then the
  same contract flow bootstrap runs.
- **oracle** `verify.py` — the derived identity must equal the pinned
  `EXPECTED_USER_ID`, and the node must answer as that User with an active
  contract.

**Saves nothing, and stays out of `main.suite`.** Two reasons, and either
alone is sufficient:

- A state has at most one producer. A second producer of `one-node` would
  silently shadow bootstrap's (migration plan, open decision 3).
- `start = "null"` means a pristine node. In env `node` the chain is one
  live session, so once bootstrap has claimed node1 there is no way back to
  an unclaimed one mid-suite.

Run it alone: `./tests/run import-user-software-key`.

The pinned identity is the oracle's whole point. BIP-39 plus BIP-32
`m/44'/0'/0'/0/0` is deterministic, so an equality against a constant proves
the existing key was used and not a fresh one — the check the netsim
`verify.sh` could only make when its caller supplied `ASTRAL_USER_ID`.
