# adopt-claimed-node-setup

The setup of `adopt-claimed-node`: a second User, B, owns a two-node swarm
beside the first User's.

- **after** `object-store-peer`, in `adopt-node`'s world (node1 and node2 in
  the first User's swarm) · **no destroys** — it adds `acn-b1` and `acn-b2`,
  and touches no node another test names. Hung under `object-store-peer`
  because a second non-destroying producer under `adopt-node` would fork a
  world, which `selftest/test_plan.py` allows only for a destroyer.
- **roster** `acn-b1`, `acn-b2` (fresh nodes no other test uses).
- **driver** `script.py` — on `acn-b1`, a software key becomes User B
  (the `bootstrap-user-software-key` chain); the two nodes learn each other's
  endpoint, B links to `acn-b2` and adopts it. Facts: B's identity and
  token, and the id of the contract `acn-b2` holds.
- **oracle** `verify.py` — both nodes are `claimed` under B, not the first
  User; `acn-b2` holds the recorded contract; each lists the other linked;
  node1's swarm lists neither.

Its own test, not a step of `adopt-claimed-node`, so that an operator drives
only the flow under test (`prompt.md`), and the prereqs run by script.
