# swarm-state-app-on-member

Criterion 2.2: an app on a Member node reaches the wallet after the User
approves on a Privileged node, and the wallet still decides its limits.

- **after** `swarm-state-member` (node3 a Member) · **host_listener** — the
  wallet stand-in serves on a test-host port node2 dials.
- **driver** `script.py` — the wallet stand-in (`tests/e2e/internal/swarmapps`,
  `wallet`) registers on node2; the User approves its ContractRequest on
  node1, and the wallet, once its contract is signed, advertises `wallet` as
  private and answers `wallet.spend` up to 100 itself. The front app
  registers on node3 asking a ServiceDiscovery permit for `wallet`. Once the
  User sees the wallet, the front's swarm discovery and a direct spend of 50
  are recorded; the User
  approves the front's request on node1; the driver polls the front's swarm
  discovery until it shows the wallet (up to 60 s), and the front spends 50
  and 500. Records `app_on_member`.
- **oracle** `verify.py` — no wallet and no spend before the Approval; the wallet within
  60 s after it, and live at the oracle; the request approved on node1 and
  node3; node3's contract for the front carries node1's Approval of that
  request; 50 spent, 500 refused by the wallet's own handler.

"Reaches" is both discovery of the private offering and a direct call of
the wallet's identity: services' query gate refuses a query to a provider of
a private service unless the caller holds a discovery permit for it on that
node, walked through its chain. Before the Approval the front's direct call
is refused on node2.

**Strictness.** With every discovery permit granted (`callerView.permitted`
returning true) the oracle fails: "the front saw the wallet before any
Approval". With the carried discovery admitted by the sending node's own
discovery permit, as before this branch's fix in `mod/services/src/op_discover.go`,
it fails: "the front's swarm discovery never showed the wallet within 60 s
of the Approval". With the query gate disabled (`PreprocessQuery` in `mod/services/src/query_gate.go` returning nil) it fails: "the front spent before any Approval".
