# two-swarms-side-by-side

Two Users' swarms coexist and stay apart. User A's swarm (node1, node2) and
User B's swarm (tssb1, tssb2) stand side by side, node2 holds a live link to
tssb1, and every node's sibling and roster lists stay exactly its own swarm's
over a window. No contract issued by the other User is taken, and a linked node
of the other swarm reads nothing of the swarm.

- **after** `adopt-node` (node1 and node2 in User A's swarm) · **destroys**
  `true` — see below. Roster: `node1`, `node2`, `tssb1`, `tssb2`; the `tssb`
  prefix is this test's own.
- **driver** `script.py` — `tssb1` is claimed for User B by a token-less
  `user.bootstrap`; B adopts `tssb2` (explicit endpoints, `new_link`,
  `user.adopt`, as adopt-node does); node2 learns tssb1's address and links
  to it. The driver then offers four contracts with `objects.push` from a
  node's own token: node2's contract for A pushed to tssb1 and tssb2's
  contract for B pushed to node2 (across), and the same two pushed to a node of
  their own swarm, node2's from node1 to node2 and tssb2's from tssb1 to tssb2
  (controls). It records each node's answer as a fact (`pushes`).
- **oracle** `verify.py` — reads every node as its own swarm's member (A's
  token on node1, B's on tssb1, node tokens on node2 and tssb2).
  1. Within 30 s each swarm is whole: `user.list_siblings` is the other node,
     `user.swarm_status` the two nodes with the other linked, `user.info` the
     node's own User, and node2 and tssb1 hold a link to each other.
  2. For 15 s, polling every second, the same sets stay exactly equal, node2
     keeps its link to tssb1, and no node holds a link into the other swarm
     except node2 and tssb1 to each other.
  3. Across the link, node2 asking tssb1 and tssb1 asking node2 for
     `user.list_siblings` and `user.swarm_status` are each refused with
     `not permitted`; User B's token on tssb1 asking node2 is refused (`not
     permitted` or `query rejected`).
  4. The two crossing pushes were refused, and the two controls were taken.

## What each check guards

- The positive check keeps the exact sets from passing on four lone nodes.
- The window is for a sync that arrives late: a link between swarms
  schedules no roster sync today (`ReceiveObject` syncs only a node already in
  the local swarm, mod/user/src/object_receiver.go), and a change that did
  would add a sibling some seconds after the link.
- The crossing pushes are the only observable of a node taking a foreign
  contract. `checkNodeContract` refuses a node contract whose issuer is not the
  active contract's issuer (mod/user/src/object_receiver.go); an accepted B
  contract changes no sibling list on node2, because `ActiveNodes` filters on
  the active contract's issuer, so only the push answer shows it. The controls
  show the push path accepts what it should, so a refusal means a decision.
- The cross-swarm reads are the SeeSwarm guard (`AuthorizeSeeSwarm`): it admits
  the swarm's User and the swarm's nodes, not any linked node.

## Place in the plan

The test declares `destroys`: node2 ends with a live link to a node of another
User and the world holds a second swarm, which a later test counting links or
providers on node2 would read. It follows `adopt-node` and so forks into a
world of its own that replays `bootstrap-user-software-key → adopt-node` as
prereqs; w0 and its roster limit are untouched, and no sibling test runs on a
node this test linked to B. `swarm-authz-matrix` also claims a second User's
node (`stranger`) in w0, but it probes what a linked stranger may do to node1,
where this test asserts the sets of two whole swarms.
