# services-swarm-outcome-rules

A stranger linked to node2 is not refused discovery: it sees nothing and reads
the service in `services.not_granted`. It is refused a discovery in an app's
name and cannot make node1 carry a discovery to the swarm. Across the swarm,
`services.incomplete` names a service a member reports incomplete alone, and
the member's unresolved services when it does not conclude inside the host's
budget.

- **env** node · **start** `adopt-node`'s world (node1, node2 adopted;
  `ssor_stranger` a fresh third node) · **saves** — (an observer: node1 and
  node2 each get one more link, from the stranger)
- **driver** `script.py` — links the stranger to node1 and node2 (it dials), then runs `driver/` (Go, this repository's pinned astral-go) in
  `drive` mode. A provider app on node2 advertises three services: `ssor-open`
  answers, `ssor-slow` never answers and stays bound, `ssor-gone` never
  answers and unbinds once asked. It records how each discovery came back:
  1. node1's node token asks node2 for `ssor-open`: the member's view;
  2. the stranger's node token asks node2 for the same: the stranger's view;
  3. both send node2 a discovery `for` the User, tagged, the form a hosting
     node sends when it carries a swarm discovery;
  4. the stranger asks node1 for `reach=swarm`;
  5. an app on node1, granted discovery of the three names, discovers with
     `reach=swarm`: `ssor-open` alone; `ssor-open` with `ssor-slow`;
     `ssor-open` with `ssor-gone`.
  Every binding closes before the driver ends.
- **oracle** `verify.py` — judges the record, then asks the driver to record
  again with fresh identities and judges that too (`driver verify`).

## Why the oracle records again

astral-py cannot speak the Services wire, so a discovery's outcome is only
visible through the Go client. An oracle that read nothing but the driver's
record would have no view of its own. The second record also proves the driver
left no binding behind: it advertises the same names, and node2 refuses a name
that is still bound.

## The rules

From astral-docs `protocols/services/ops/services.discover.md` (pinned in
`.ai/system`) and `mod/services/src`, as master stands after #414 (provider
visibility):

| asker, sent to | outcome |
|----------------|---------|
| node1's node, `ssor-open` on node2 | the offering; no `not_granted`; bare `eos` |
| the stranger, `ssor-open` on node2 | no offering; `not_granted [ssor-open]`; bare `eos` — admitted, never refused |
| node1's node, for the User | the offering, tagged open |
| the stranger, for the User | `error_message`, "not permitted to discover for this app" |
| the stranger, `reach=swarm` on node1 | `error_message`, "reach=swarm is accepted only from a local app" |
| an app on node1, `reach=swarm`, `ssor-open` | the offering; bare `eos` |
| the same app, `ssor-open` and `ssor-slow` | the offering; `incomplete` naming `ssor-slow` (and `ssor-open`); not before node1's 5 s budget ends |
| the same app, `ssor-open` and `ssor-gone` | the offering; `incomplete [ssor-gone]` alone, at once |

The stranger's plain discovery reads as empty because the caller is neither the
User, a swarm node, nor an app hosted on one, and holds no permit
(`isSwarmCaller`, `callerView.sees`). node1's node token is a swarm node, so it
sees the open provider; that control shows the stranger's empty answer comes
from who asks, not from the provider. The refusal of a `for` discovery has two layers in `admitCarry`: only a swarm
node may relay for the User (`AuthorizeRelayFor`), and the sending node needs
discovery permission for each requested service (`authorizeDiscovery`). Either
refuses the stranger with a message that begins "not permitted", so the test
pins the prefix and the refusal, not which layer spoke.

## Why `incomplete` names two services for a silent member

node1 asks node2 once for both services and waits for node2's single outcome.
Its budget starts before node2's, so when node2 never concludes node1 gives up
first and fails every service of node2's still unresolved, the answered one
included (`budgetExpired`). The test pins that `ssor-slow` is named, that
nothing else outside the two asked is named, and that the answered offering is
kept. When node2 reports, as it does for `ssor-gone` the moment its provider
leaves, only the failed service is named.

## Why `after = "adopt-node"` and a stranger of its own

`swarm-authz-matrix` links its `stranger` to node1 only, so this test needs a
link to node2 either way. Continuing that test would make it a producer, and
the planner would move it out of the shared world into a fork of its own, which
costs a world and rewrites the pinned plan. The stranger here is a fresh node,
`ssor_stranger`: node2 judges the asker's identity, which is neither the User,
a swarm node, nor an app on one, whatever state the asker's own node is in.
Services are named `ssor-*`, not `player`, and every binding is closed, so no
later test sees a provider this one left.
