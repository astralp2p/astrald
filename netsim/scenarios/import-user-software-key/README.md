# import-user-software-key

Tests importing an existing user identity into a node from a recovery phrase.

- **Kind:** scenario · **Family:** identity
- **Chain:** `astrald-lab` → `one-node`
- **Steps:** import-user-software-key
- **Run:** `netsim story --stage astrald-lab --save one-node netsim/scenarios/import-user-software-key/import-user-software-key.story`

Takes an existing recovery phrase and uses it to rebuild the keys that make the node a user node with an active contract, then confirms the node identifies as that user. This is the alternative to bootstrap-user-software-key (both yield the one-node state) for the recover-an-identity path.
