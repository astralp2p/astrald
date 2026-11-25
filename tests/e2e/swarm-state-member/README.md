# swarm-state-member

The setup test of `swarm-state.suite`: node3 joins the swarm of node1 and
node2 as a Member, through node1, after the User approves its JoinRequest.

- **after** `adopt-node` (node1 and node2 Privileged) · **destroys** `true` —
  node3 joins the swarm, so every later test in the world sees three members.
- **driver** `script.py` — node1 and node3 learn each other's endpoint; node3
  runs `user.join?tier=member` through node1 while the User, on node1, finds
  the request with `user.swarm_requests` and approves it with
  `user.answer_request`. node2 and node3 then learn each other's endpoint,
  so every pair can link: the later scenarios test authority, not meshing.
  Records `member_join`: the request's ID, its status and the membership
  node3 received.
- **oracle** `verify.py` — node3's membership names node3, is issued by node1
  and carries an ApprovalID; node3 holds it as its active contract and sees
  the swarm's requests as a member; node1 lists the request as approved.
