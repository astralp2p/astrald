# expel-node

Tests permanently banning another node from a shared swarm.

- **Kind:** scenario · **Family:** network
- **Chain:** `two-nodes` → `two-nodes-expel`
- **Steps:** expel-node
- **Run:** `netsim story --stage two-nodes --save two-nodes-expel netsim/scenarios/expel-node/expel-node.story`

One node expels another: it goes on a blocklist and is dropped from the active members, and the test confirms it is blocked and no longer on the roster. This is how a swarm enforces membership and keeps out unwanted nodes.
