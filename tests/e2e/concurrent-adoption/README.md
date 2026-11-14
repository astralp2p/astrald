# concurrent-adoption

Of several Users who adopt one fresh node at the same moment, exactly one
wins. Every other adoption is refused cleanly, the node holds the winner's
contract alone, and no loser's swarm lists the node. The race runs five
times, on five fresh nodes.

- **root** (no `after`) · **destroys** `true`: every node of the roster ends
  claimed. Roster: `cadu1`-`cadu4` (the adopters), `cadp1`-`cadp5` (the
  prizes); the `cad` prefix is this test's own.
- **driver** `script.py` — each `cadu` node is claimed by a User of its own
  (token-less `user.bootstrap`). For each prize in turn, every adopter node
  learns the prize's address and links to it, the prize learns each
  adopter's, and then all four Users call `user.adopt` of the prize at once,
  each on its own node, from connections opened before the start. Each answer
  and its start and end time (ms from the start) is the fact `rounds`; the
  Users are the fact `adopters`.
- **oracle** `verify.py` — per round:
  1. exactly one adoption was answered with a contract; each other was
     refused as `node already claimed` or `session closed` (the refusal
     reason can be lost, see `adopt-refusals`);
  2. the prize's `user.info` is `claimed` under the winner's User, its
     `contract_id` is the contract the winner was answered, and the contract
     the prize serves to the winner's node is the same object;
  3. within 30 s the winner's `user.swarm_status` and the prize's are both
     exactly the winner's swarm: the winner's node and every prize that User
     won; at no poll, and for 5 s after, does a loser's `user.swarm_status`
     or `user.list_siblings` list the prize.

  A failing round adds the prize's log lines about the contracts it took
  (`[user] hello, …`, one per contract it set active) to the verdict.

## What it guards

`user.accept_membership` (mod/user/src/op_accept_membership.go) checks that
the node has no active contract when the query arrives and sets the contract
at the end of the ceremony. Without a lock around the check and the set, every
adopter that arrives inside that window passes the check and is answered with
the node's signature, and the node keeps whichever contract it set last. The
oracle then sees more than one winner, a `user.info` that disagrees with
some winner, and a loser's swarm that lists the node.

## Place in the plan

A root test that destroys: it forks a world of its own with nine fresh nodes,
so no other test's roster is touched. It is in `main.suite` and
`integration.suite`, and needs nothing but the session view and
`lib/nodefs.py` (for the diagnostic log lines), so it runs in a local or a
Docker world.
