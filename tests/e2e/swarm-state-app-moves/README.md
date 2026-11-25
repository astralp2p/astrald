# swarm-state-app-moves

Criterion 2.3: an app moved to another device gets its access back through a
re-issued contract, after a fresh request there.

- **after** `swarm-state-expel-cuts-chains` (node3, the front's device, is
  expelled, and the front's chain through it is cut).
- **driver** `script.py` — the front moves to node2 as a backup restored
  there: its key is copied from node3's object store (`lib/nodefs`) to node2
  (`objects.store`, which node2's crypto module indexes), and node2 mints a
  token for it. The front's local discovery on node2 is recorded; the front
  calls `apphost.request_contract` on node2 with the permits it asked on
  node3, and the User approves the fresh request on node1. The driver polls
  the front's discovery until it shows the wallet (up to 60 s). Records
  `app_moves`.
- **oracle** `verify.py` — no wallet on arrival; the request on node2 is a
  new entry, filed by node2 for the front, approved; the contract
  `apphost.request_contract` returned is issued by node2 for the front and
  carries an Approval of that request; the wallet is back within 60 s and
  live at the oracle; the wallet spends 50 for the front.

**Strictness.** Without the key on node2 the host worker cannot sign the
contract ("sign as subject: no signing scheme available for key"), and the
oracle failed: "apphost.request_contract answered {... 'error': ...}" (run
`20261125T221055Z`). The arrival check depends on the expel cutting the old
chain, which `swarm-state-expel-cuts-chains` shows fails without it.
