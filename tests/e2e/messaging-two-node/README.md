# messaging-two-node — mail and its receipt cross a link

ann's mailbox is hosted on node1 and ben's on node2. ann writes to ben, ben
reads it, node2's receipt stamps ann's row on node1, and ben's answer reaches
ann.

- **env** node · **start** `two-nodes` · **saves** —
- **driver** `script.py` — mint ann on node1 and ben on node2 with
  `messaging.create_identity`; node2 pushes ben's relay contract to node1; ann
  sends, ben waits and reads, and ben answers.
- **oracle** `verify.py` — node1 took ben's relay contract, each node's
  authority was asked its own participant's side, and the rows on both nodes
  record the landing, the receipt and the answer.

## What this proves that the unit tests do not

The unit tests route a delivery to a module in the same process. Here the
delivery leaves node1 as a relay query that names ann as the caller and ben as
the target, node2 admits node1's relay for ann, and mod/messaging on node2
stores the message. The receipt makes the same trip the other way.

## Why node2 introduces ben to node1

node1 routes to an identity it does not host through a relay contract that
identity issued: mod/apphost's query preprocessor names the contract's subject
as a relay. `messaging.create_identity` signs one for ben, but only node2 holds
it. node2 pushes it with `objects.push`, and node1 indexes it because the
sender is the sibling the contract names as subject (mod/user's object
receiver). The oracle checks node1's answer to the push first: without it the
exchange never had a route.

The way back needs no step. node1 pushes ann's relay contract to node2 as the
caller proof of the first relayed delivery, so node2 already knows where ann is
hosted when the receipt and the answer leave.

## Why each node serves its own authority

A message asks `mod.messaging.send_action` of its sender on the sending node
and `mod.messaging.receive_action` of its recipient on the receiving node. Each
node names its own authority, and each authority here admits only its own
participant's side. A question put on the wrong node is refused there, and the
oracle reads both records.
