# swarm-state-expel-cuts-chains

Criterion 2.7: expelling the Member node cuts every app chain through it.

- **after** `swarm-state-app-on-member` · **destroys** `true` — node3 is
  expelled.
- **driver** `script.py` — node2 mints a token for the front app's
  identity, so the front's chain (node3's contract under node1's Approval)
  is asked on node2, the wallet's node, without node3. The front's
  discovery of the private wallet is recorded on node3 and on node2
  (local); the User expels node3 on node1 (`user.expel`); once node1 and
  node2 store the Expulsion, the driver polls the front's discovery on node2
  until it shows no wallet (up to 60 s). Records `expel_cuts`.
- **oracle** `verify.py` — before the expel the chain held on node3 and
  node2; node1 and node2 store the Expulsion; within 60 s, and live at the
  oracle, the front sees no wallet on node2; node1 and node2 still store
  node3's contract for the front, so the expel of its issuer is what cuts
  the chain.

There is no swarm discovery from node1: a Privileged node relays as the user
only, so node2 refuses node1's carry of
an app node3 hosts whatever its chain.

**Strictness.** The expel cuts the chain three times: the request filed by
an expelled node reads declined (`swarmSnapshot.declined`); the Approval link
refuses an expelled issuer, subject or approver (`swarmWalk.approvalLink`,
its Expelled loop); and the walk asks the issuer's veto itself
(`mod.vetoed(sc.Issuer)` in `swarmWalk`, mod/auth/src/swarm_walk.go), which
the user module answers for an expelled node. Any one of them is enough: with
the first two disabled the test still passes (run `20261126T033600Z`), and
with all three disabled the oracle fails: "60 s after the expel the front
still saw the wallet" (run `20261126T033657Z`).
