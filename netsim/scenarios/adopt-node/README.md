# adopt-node

Joins two nodes into the same user's network so they trust each other.

- **Kind:** fixture · **Family:** foundation
- **Chain:** `one-node` → `two-nodes`
- **Steps:** adopt-node
- **Run:** `netsim story --stage one-node --save two-nodes netsim/scenarios/adopt-node/adopt-node.story`

One node brings a second node into its personal network as a sibling, then verifies both share the same user contract and see each other as linked peers. This is the stable two-node baseline the multi-node scenarios start from.
