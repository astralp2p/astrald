# expel-cross-traffic

After the User bans node3, no relay contract push, no swarm discovery and no
relayed query crosses the ban, in either direction, while apps and swarm
follows that used those paths stay live through it.

- **after** `swarm-roster-sync` (node1, node2, node3 in the User's swarm) ·
  **destroys** `true` — a ban cannot be lifted · **host_listener** — the apps
  serve handlers on the test host.
- **driver** `script.py` with the Go program `driver/` — the Go program serves
  a pinger app on node1 and one on node3, a provider of the service
  `expel-cross-traffic` on node1, and swarm follows of that service on node2
  and node3 (each granted on its own node). Once both follows hold the
  provider's offering and node2 names node3 as the host of node3's app, the
  User expels node3 on node1. Once node2 holds the ban, the Go program asks
  node2's `apphost.hosts` for both apps and pings both from node2; node2's
  links to node3 are listed before and after. The flow holds 12 s after the
  expel. Every node's log is tapped across the flow (`lib/nodelog.py`).
- **oracle** `verify.py` —
  - setup was live: node3's carry ran `services.discover` on node1, and
    node2's carry ran it on node3, before the ban;
  - every node logged the ban; from 1 s after it on each node, node1 and node2
    log no `services.discover` with node3 and reject no pushed contract from
    node3, and node3 logs no `services.discover` with node1 or node2 and
    rejects no pushed contract from them;
  - node2's `apphost.hosts` names node1 for node1's app and no host for node3's
    app; node1's app answers node2 within 5 s; the query to node3's app fails,
    node3's app serves nothing, and node2 opens no new link to node3 while it
    routes.

Spec: `.ai/system/topics/app-routing.md` (Resolving the host, Authorizing the
relay). Decision: A2G8 option b. Ticket:
https://wiki.satforge.dev/doc/qHhjbojGwM

## What it does not judge

- A link node2 holds to node3 just after the ban. node2 re-links within
  milliseconds of node3 tearing its links down; refusing that link is the link
  admission ticket's work (expel-propagation reports the link count). The
  probe judges only links opened while node2 routes.
- The inbound relay check (`mod/nodes` `admitsRelayer`). In this flow node3's
  caller proof push is refused first, so a relayed query from node3 never
  reaches a member; the unit tests in `mod/nodes/src` hold the check.
- One app identity hosted on two nodes. `apphost.register` mints an identity
  per node (`topics/app-routing.md`, The contract an app signs), so the probe
  uses one app on node1 and one on node3.

## Observed

A member's follow may retry its carry to node3 once in the first tens of
milliseconds after the ban, when node2 re-links to node3 before its member
set refreshes: a run logged `push relay contract … route not found` and one
`services.discover` on node3 26 ms after node2 removed node3. The 1 s grace
leaves that in-flight retry out; a carry that keeps retrying shows within it.
