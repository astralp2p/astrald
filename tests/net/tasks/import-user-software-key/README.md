# import-user-software-key

node1's agent makes node1 a User node from the BIP-39 mnemonic in `prompt.md`, deriving the existing key and installing its active contract. verify.sh asserts `apphost.whoami` reports that User id and `user.info` finds an active contract (matching `ASTRAL_USER_ID` if set). astrald-lab → one-node.
