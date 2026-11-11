# adopt-refusals

After: adopt-node (node1 and node2, one User). The User asks node1 to adopt
an alias nothing carries, the zero identity (by the name `anyone` and by its
66 zeros), and node2 again. Each is a refusal: `unknown identity` and
`zero identity` are the op's own answers. The re-adopt is refused by node2's
accept_membership (node1 signs the membership, nothing is indexed); it
reaches the User as `node already claimed`, or as `session closed` when node2
closes the session before node1 has written the contract (an astrald race,
about half the runs), and the oracle takes either, never a success. The oracle checks those answers, then reads the
swarm itself: the identities node1 and node2 list are exactly what they were
before, and each node's active contract is the same object.

The driver records the pre-state and the answers as facts; the oracle reads
the post-state live. No node is restarted or destroyed, so the world stays
usable for later tests.
