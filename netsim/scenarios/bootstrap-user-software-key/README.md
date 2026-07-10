# bootstrap-user-software-key

Turns a node into a user-controlled node by creating a fresh user identity.

- **Kind:** fixture · **Family:** identity
- **Chain:** `astrald-lab` → `one-node`
- **Steps:** bootstrap-user-software-key
- **Run:** `netsim story --stage astrald-lab --save one-node netsim/scenarios/bootstrap-user-software-key/bootstrap-user-software-key.story`

Creates a user account on the node and activates it with a contract, then confirms the node recognizes the user and accepts user commands. The result is a working user-controlled node that later scenarios build on.
