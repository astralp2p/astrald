# object-store-peer

Tests that one node can store a data object on a peer and get it back.

- **Kind:** scenario · **Family:** objectstore
- **Chain:** `two-nodes` → `two-nodes-data-peer`
- **Steps:** object-store --target node2
- **Run:** `netsim story --stage two-nodes --save two-nodes-data-peer netsim/scenarios/object-store-peer/object-store-peer.story`

One node creates a file-based object and stores it on a connected peer node, then confirms the peer can serve back the exact same data. Shows the object store works across two connected nodes.
