# swarm-state-expel-cuts-chains

Criterion 2.7: expelling the Member node cuts every app chain through it.

- **after** `swarm-state-app-on-member` · **destroys** `true` — node3 is
  expelled.
- **driver** `script.py` — node1 and node2 each mint a token for the front
  app's identity, so the front's chain (node3's contract under node1's
  Approval) is asked on those nodes without node3. The front's discovery of
  the private wallet is recorded on node3, on node2 (local) and from node1
  (swarm); the User expels node3 on node1 (`user.expel`); once node1 and
  node2 store the Expulsion, the driver polls the front's discoveries on
  node1 and node2 until neither shows the wallet (up to 60 s). Records
  `expel_cuts`.
- **oracle** `verify.py` — before the expel the chain held on all three;
  node1 and node2 store the Expulsion; within 60 s, and live at the oracle,
  the front sees no wallet on node2 or from node1; node1 and node2 still
  store node3's contract for the front, so the expel of its issuer is what
  cuts the chain.

**Strictness.** The expel cuts the chain twice: the request filed by an
expelled node reads declined (`swarmSnapshot.declined`), and the Approval
link refuses an expelled issuer (`swarmWalk.approvalLink`, `vetoed`). With
only the second disabled the test passes; with both disabled the oracle
fails: "60 s after the expel the front still saw the wallet".
