# object-store

node1's agent stores `payload.txt` as an Object via `objects.store` on `--target` (default `localnode`; an alias stores on that peer) and records the id. verify.py re-loads the id with `objects.load -repo local` on the holder and asserts the bytes equal `payload.txt`. two-nodes → two-nodes-data (localnode) or two-nodes-data-peer (`--target node2`).
