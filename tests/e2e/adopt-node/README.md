# adopt-node

Start: one-node → saves: two-nodes. The driver registers explicit loopback
endpoints on both nodes (no LAN discovery exists here), opens a direct tcp
link (strategies=basic), adopts node2 into the User's swarm, and registers
node1/node2 dir aliases on both. The oracle is the netsim adopt verifier
ported: same-issuer contracts on both nodes, symmetric linked-sibling
roster, and a live link from node2 back to node1.
