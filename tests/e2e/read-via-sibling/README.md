# read-via-sibling

An app on node1 reads an object only node2 holds, by asking node1 alone.
node1 authorizes the app, misses the object on its own device, and its
`siblings` repository reads it from node2's device repository under node1's
identity.

- **env** node · **start** `two-nodes-data-peer` · **saves** —
- **driver** `script.py` — mint a Player app and an evaluator app on node1,
  register an app on node2 and ask node1's `apphost.hosts` about it, set an
  evaluator rule for the Player's `mod.auth.see_objects_action`, serve
  `auth.evaluate`, then read the object as the Player three times.
- **oracle** `verify.py` — node1's `apphost.hosts` names node2 for node2's
  app and node1 for the Player; the allowed read returns node2's bytes and
  node2's log records the read; the refused read is rejected with code 1 and
  node2's log records none; the unlinked read is rejected with code 5 within
  its budget; node1's device repository lacks the object afterwards.

## The three phases

| phase | evaluator | node2 | expected |
|-------|-----------|-------|----------|
| allowed | acks | linked | node2's bytes, node1's device still lacks the object |
| refused | answers `eos` | linked | rejected with code 1 (`astral.CodeRejected`), no `objects.read` reaches node2 |
| unlinked | acks | unlinked | rejected with code 5 (`objects.CodeUnavailable`) within 10 s |

The Player reads with no target, so every read is node1's to route. node1
asks the evaluator before it opens any repository, so a refusal never reaches
the siblings repository.

## Why the evaluator

The Player holds no grant and no contract for `mod.auth.see_objects_action`,
so the evaluator rule is the only authority that can allow its read. Each
question the evaluator answers is recorded with its payload, and the oracle
checks that every question names the Player and the object.

node2 never sees the Player: the siblings repository reads as node1, and node2
grants a current sibling the read of one object from its device repository.

## Why node2's log

"No query reaches node2" is a claim about node2, and node2's log is the one
record of what it was asked. The driver records the bytes of node2's log that
the allowed and refused phases span. The allowed phase is the control: its read
must appear in node2's log, or the absence in the refused phase proves nothing.

## Unlinking

Both nodes maintain a link to each other, so closing it alone is answered by a
new link within milliseconds. The driver turns off `dial` and `listen` for
`tcp` and `kcp`, and `dial` for `tor`, on both nodes through
`/mod/<module>/settings/<knob>`. It then closes node1's links to node2 until
node1 holds none for one whole second, and reads. Linking is restored and the
link rebuilt in a `finally`, so the tests that follow find the `two-nodes`
world they expect. The oracle checks that the link came back.

Dialling alone was not enough: in one `main.suite` run node2 built a new `tcp`
link to node1 milliseconds after node1 closed the old one, with `dial` already
off on both nodes. Turning `listen` off as well closes that path whichever
side dials.

## Host lookup

Before the reads, the driver registers an app on node2 while node1 is linked
to node2. `apphost.register` pushes the app's relay-for contract to node2's
swarm, so node1's `apphost.hosts` must name node2 for that app within 10 s.
The driver asks as the Player, which holds no permit, because the op asks for
none. node1's links to node2 are recorded before and after: an unchanged link
set rules out a new link's contract sync as the carrier. node1's
`apphost.hosts` must also name node1 for the Player, and nothing for node1's
own identity, which is no app.
