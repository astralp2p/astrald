# read-remote-peer

Stores data on a peer, then reads it back over the network.

- **Kind:** scenario · **Family:** objectstore
- **Chain:** `two-nodes` → `two-nodes-peer-read`
- **Steps:** object-store · read-remote-object
- **Run:** `netsim story --stage two-nodes --save two-nodes-peer-read netsim/scenarios/read-remote-peer/read-remote-peer.story`

One node stores a file on a peer and notes which object was created, then reads that object back from the peer across the link. Verifies astrald can move and fetch data between different nodes.
