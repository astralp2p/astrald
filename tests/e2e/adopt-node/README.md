# adopt-node

Start: one-node → saves: two-nodes. The driver registers explicit loopback
endpoints on both nodes (no LAN discovery exists here), opens a direct tcp
link (strategies=basic), has node2 join the User's swarm through node1
(user.join, confirmed by the User with user.confirm_join), and registers
node1/node2 dir aliases on both. The oracle is the netsim join verifier
ported: same-issuer contracts on both nodes, symmetric linked-sibling
roster, and a live link from node2 back to node1.
