# messaging-two-node — mail, its receipt and its refusal cross a link

ann's mailbox is hosted on node1, and ben's and cal's on node2. ann writes to
ben, ben reads it, node2's receipt stamps ann's row on node1, and ben's answer
reaches ann. Then ann writes to cal, whom node2's authority lets receive from
nobody, and node2's refusal reaches ann with its own words. Last, ann asks ben
for a path nothing on node2 serves, and node2's missing route reaches ann as a
missing route.

- **env** node · **start** `two-nodes` · **saves** —
- **driver** `script.py` — mint ann on node1, and ben and cal on node2, with
  `messaging.create_identity`; node2 pushes ben's and cal's relay contracts to
  node1; ann sends, ben waits and reads, and ben answers; ann sends to cal;
  ann queries `e2e.no_such_path` on ben.
- **oracle** `verify.py` — node1 took both relay contracts, each node's
  authority was asked its own participant's side, the rows on both nodes
  record the landing, the receipt and the answer, and ann's send to cal
  answered `delivery failed: the recipient does not take messages from you`
  and left those words on her failed row, and ann's query of the unknown path
  ended `RouteNotFound`.

## What this proves that the unit tests do not

The unit tests route a delivery to a module in the same process. Here the
delivery leaves node1 as a relay query that names ann as the caller and ben as
the target, node2 admits node1's relay for ann, and mod/messaging on node2
stores the message. The receipt makes the same trip the other way.

The refusal takes the same path. node2 rejects the delivery to cal with
`RejectNotAdmitted`, code 5, the link carries the code to node1, and node1's
relay path answers that rejection instead of a missing route. A sender reads a
refusal across nodes as it reads one on its own node.

The unknown path takes the same path too, and it guards the other side of that
rule. node2 has no route for it and answers the generic reject code 1 over the
link, because a link response has no code of its own for a missing route.
node1's relay path counts code 1 as no route, so ann reads `route_not_found`
and not a refusal.

## Why node2 introduces ben and cal to node1

node1 routes to an identity it does not host through a relay contract that
identity issued: mod/apphost's query preprocessor names the contract's subject
as a relay. `messaging.create_identity` signs one for each participant, but
only node2 holds ben's and cal's. node2 pushes each with `objects.push`, and
node1 indexes it because the sender is the sibling the contract names as
subject (mod/user's object receiver). The oracle checks node1's answers to the
pushes first: without them neither send had a route.

The way back needs no step. node1 pushes ann's relay contract to node2 as the
caller proof of the first relayed delivery, so node2 already knows where ann is
hosted when the receipt and the answer leave.

## Why each node serves its own authority

A message asks `mod.messaging.send_action` of its sender on the sending node
and `mod.messaging.receive_action` of its recipient on the receiving node. Each
node names its own authority, and each authority here admits only its own
participant's side. A question put on the wrong node is refused there, and the
oracle reads both records. node1's authority admits ann's send to cal, so the
refusal the oracle judges is node2's.
