# swarm-state-app-own-node

Criterion 2.4: an app reaching a service on its own node is allowed by its
contract chain, and denied without it.

- **after** `swarm-state-member` (node3 a Member) · **host_listener** — the
  wallet stand-in serves on a test-host port node3 dials.
- **driver** `script.py` — a wallet stand-in registers on node3, the User
  approves it on node1, and it advertises `wallet` as private on node3. Two
  apps, `front` and `other`, register on node3, each asking a
  ServiceDiscovery permit for `wallet`. Once the User sees the wallet, each
  app's local discovery (reach `local`: node3 alone), a direct spend by
  front, and front's try to serve and advertise are recorded; the User
  approves front's request and declines other's; the driver polls front's
  local discovery until it shows the wallet (up to 60 s), then front and
  other each try to spend 50, and other tries to serve and advertise.
  Records `app_own_node`.
- **oracle** `verify.py` — neither app saw the wallet before an answer;
  front sees it within 60 s and live at the oracle, and spends 50; node3
  stores a contract it issued for front carrying an Approval of front's
  request; other, declined, sees no wallet at the driver and at the oracle,
  and its request reads declined; the query gate refused front's spend
  before its Approval and other's after its Decline, each with the gate's
  own `query rejected (1)` (any other error fails); front while pending and
  other once declined are refused `apphost.register_handler` and a private
  `services.advertise` with `not permitted` (no ServeApps without an approved
  request), and front, approved, does both at the oracle as the control.
  This replaces the revoke steps G7 removed from app-call-local and
  services-advertise-identity: revocation is a Non-goal, and a pending or
  declined request is how an app goes without ServeApps now.

**Strictness.** With every discovery permit granted (`callerView.permitted`
returning true) the oracle fails: "before_front: an app saw the wallet
before any answer". With the query gate disabled (`PreprocessQuery` in `mod/services/src/query_gate.go` returning nil) it fails: "front spent before any answer".
