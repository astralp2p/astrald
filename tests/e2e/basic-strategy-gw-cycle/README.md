# basic-strategy-gw-cycle

A basic round to a node behind gateways that reach each other only through
each other ends within the caller's 60 s budget and does not loop.

- **after** `bootstrap-user-software-key` · **roster** `node1`.
- **driver** `script.py` — node1's book gets three strangers
  (`lib/strangers.py`): the target T at `gw:A:T`, gateway A at `gw:B:A`, and
  gateway B at `gw:A:B`. Two `nodes.new_link` rounds to T as the User on
  node1, `strategies=basic`, with a 90 s caller timeout, and an
  `apphost.whoami` read after each. Judges nothing.
- **oracle** `verify.py` — each round answered `link not produced`, no link,
  after 15 to 60 s; node1 answered `whoami` within 5 s after each; node1
  holds no link to T, A or B.

why the chain stalls: a gw dial routes a query to its gateway
(`mod/gateway/src/dial.go`), which starts a basic round for the gateway. The
round for A dials `gw:B:A`, the round for B dials `gw:A:B`, and that query to
A waits on A's round, which is still running: a link round ignores a second
signal and the caller waits for the running round
(`mod/nodes/src/basic_link_strategy.go` `Signal`). Only basic's 20 s gw row
budget ends the wait (`transportProfiles`).

why the bounds: a round that drops gw rows answers at once (astrald
`463a4fea`: 0.004 s). Without the gw budget the round waits for the 60 s
routing cap and answers after 60.01 s. Live, with the budget: 20.02 s for
each round.

Not covered: a gateway reachable through a second, live gateway. That needs
four nodes, and the netsim lab runs three (node1, node2, reflector).
