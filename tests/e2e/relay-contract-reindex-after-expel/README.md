# relay-contract-reindex-after-expel

A node that rebuilds its auth index from its repository does not bring back
the app contract of a host the User expelled.

- **after** `adopt-node` (node1 and node2 in the User's swarm) ·
  **destroys** `true` — the User expels node2 · **restart** `node1`.
- **driver** `script.py` — node2 registers an app, and node1 names node2 as
  its host; node1 registers an app of its own; the User expels node2 on node1.
- **while_down** `while_down.py` — with node1 stopped, empties node1's auth
  index (`auth__contracts`, `auth__contract_permits` in `data/astrald.db`); the
  contracts stay in node1's repository and the expulsion in its user tables.
- **oracle** `verify.py` — within 30 s of the start: `apphost.hosts` on node1
  names node1 for node1's own app, and names no host for node2's app.

At start-up the auth indexer reads every contract in the repository and
indexes it through `Auth.IndexContract`, which runs the user module's admitter
first (astrald `mod/auth/src/contracts.go`, `mod/user/src/deps.go`). The
admitter is `AdmitRelayContract` (`mod/user/src/relay_admission.go`): a relay
contract's subject must be the node itself or a member of its local swarm, and
the local swarm leaves out expelled nodes (astral-docs
`topics/app-routing.md`, Reach). Before the change the indexer checked
signatures only and re-indexed node2's contract.

The driver records what `apphost.hosts` on node1 answered for node2's app
after the expulsion and before the wipe. The index row outlives the
expulsion until a rebuild; that is outside this test.
