# basic-strategy-dead-rows

The basic strategy dials kcp and gw rows, gives a dead kcp row up after its
5 s budget, and still links node2 over its live tcp row when dead kcp and gw
rows sit beside it.

- **after** `adopt-node` (node1 and node2 linked siblings) · **destroys**
  `true` — the dead rows stay in node1's book for 90 days, and no op removes
  one · **config** `node2.kcp.listen = false`, so node2's tcp row is its only
  live one and a link's network names the row that won.
- **driver** `script.py` — a dead kcp row is `kcp:<node2's address>:<port>`
  on a port under 1024 that nothing listens on: the kcp dial succeeds at once
  and the Noise handshake never gets an answer. A dead gw row names a stranger
  gateway (`lib/strangers.py`) whose only row is a dead kcp row. Three
  `nodes.new_link` rounds as the User on node1 with `strategies=basic` and no
  endpoint, timed on the host's clock:
  - **beside**: one dead kcp row next to node2's live tcp row;
  - **stranger**: a stranger whose only row is a dead kcp row;
  - **crowd**: node2 with 10 dead rows (6 kcp, 4 gw) next to its live tcp row.
- **oracle** `verify.py` — beside answered a tcp link in under 2 s; stranger
  answered `link not produced` after 4 to 10 s; crowd answered a tcp link in
  under 30 s, and the oracle prints the time.

why the stranger bounds: a round that drops kcp rows answers at once (astrald
`463a4fea`: 0.003 s), and a round that dials a kcp row with no bound of its
own waits for the caller (30 s). A kcp dial is connectionless, and the kcp
`dial_timeout` stops applying at the first successful write
(`mod/kcp/src/conn.go`), so only basic's row budget ends the handshake
(`mod/nodes/src/basic_link_strategy.go` `defaultRowBudgets`).

why crowd measures: 10 dead rows fill basic's 8 workers, so the tcp row waits
for a dead row's budget to end. Live, on a local world: beside 0.011 s,
stranger 5.004 s, crowd 5.02 s. Without the row budgets the crowd round never
reached the tcp row and timed out after 60 s.
