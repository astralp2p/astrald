# relay-contract-forwarding

A new member learns another member's app from the member it joins through,
with no link to the app's host, and then reaches the app through that host.

- **after** `adopt-node` (node1 and node2 in the User's swarm) ·
  **destroys** `true` — node3 joins the swarm, and node2 dials no one.
- **driver** `script.py` — runs `driver/` (Go, this repository's astral-go):
  - node2 registers the greeter app, which serves `hello`, and `apphost.hosts`
    on node1 names node2 for it;
  - dialing is switched off on node2 and node3 (`/mod/tcp`, `/mod/kcp` and
    `/mod/tor` `settings/dial` in the tree), so no link between them can form;
  - node1 and node3 learn each other's endpoint, node1 links to node3
    (`nodes.new_link`), and node3 joins through node1 as a Member, which the
    User approves;
  - within 30 s, `apphost.hosts` on node3 names node2 for the greeter, while
    `nodes.links` on node3 holds no link to node2;
  - node3 may dial again; within 30 s `nodes.resolve_endpoints` on node3
    names an endpoint of node2, which node3 pulls from node1 once its sibling
    linker lists node2 as a member (`LearnMemberEndpoints`);
  - a visitor app on node3 calls `greeter:hello`;
    node3 routes the call through node2, and the greeter answers
    `hello, <visitor>`.
- **oracle** `verify.py` — every step held, and `apphost.hosts` on node3 still
  names node2 alone for the greeter.

node1's sibling sync with node3 pushes the relay contract of every app node1
holds, the ones it does not host included; without that forwarding node3
learns the greeter only from node2's own sync on a node2–node3 link, and with
no such link possible `apphost.hosts` on node3 stays empty and the driver
fails. The forwarding is in astrald `mod/user/src/sync.go` `syncAppContracts`;
node3 admits the forwarded contract by `AdmitRelayContract`
(`mod/user/src/relay_admission.go`), whose subject (node2) is a member and whose sender (node1) is a member too
(astral-docs `topics/app-routing.md`, Reach).

The visitor's call needs a node3–node2 link, so it runs after node3 may dial
again; by then node3 already holds the greeter's contract from node1. node3
may hold that contract before its sibling linker (a 3 s watch of the
SwarmHead) lists node2 as a member, and only that listing pulls node2's
endpoints, so the driver waits for an endpoint before the call.

node1 dials node3 because a joiner reaches its carrier (`user.join`) and
node3's dialing is already off. node3 joins over the link node1 opened, and node1 runs the
sibling sync with node3 once it lists node3 as a member (`onNewMember`,
`mod/user/src/module.go`).
