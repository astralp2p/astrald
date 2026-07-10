# object-store

Stores a file as an object and reads it back to check the data is intact.

- **Kind:** scenario · **Family:** objectstore
- **Chain:** `two-nodes` → `two-nodes-data`
- **Steps:** object-store
- **Run:** `netsim story --stage two-nodes --save two-nodes-data netsim/scenarios/object-store/object-store.story`

A node stores a file as an object and records its id, then reads it back by that id and checks the bytes match the original. Confirms the object store can reliably save and retrieve data on a single node.
