# adopt-node

node1's agent adopts node2 into its User swarm and saves the sibling ids to `~/siblings.json`; the host then registers the `node1`/`node2` aliases. verify.py asserts both nodes hold a contract from the same User, each lists the other as a Linked sibling, and `sibling_ids` includes node2. one-node → two-nodes.
