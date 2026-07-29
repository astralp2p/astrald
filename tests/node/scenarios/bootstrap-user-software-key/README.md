# bootstrap-user-software-key

Start: null → saves: one-node. The driver replays the node-setup op chain
deterministically: bip137sig entropy→mnemonic→seed→key, store the key,
derive the User identity, build+sign+accept the node contract, mint a User
apphost token (published as session facts user_id / user_token). The oracle
connects WITH that token and asserts whoami == user and user.info shows the
contract issued by the user for this node.

Known issue: intermittent `sign as issuer: unsupported` — a pre-existing
astrald indexing race (see tests/README.md); re-run on failure.
