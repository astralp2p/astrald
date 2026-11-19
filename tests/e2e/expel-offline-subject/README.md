# expel-offline-subject

The User expels node3 while node3 is down. When node3 comes back it learns its
ban, leaves the swarm, and the swarm follow it held stops reaching its former
siblings: no `services.discover` and no relay contract push from node3 reaches
node1 or node2 after the leave.

- **after** `swarm-roster-sync` (node1, node2, node3 in the User's swarm) ·
  **destroys** `true`: a ban cannot be lifted · **restart** `node3`.
- **driver** `script.py` with the Go program `follower/` — the follower is a
  long-lived app (`lib/apps.py`) on node3 that follows the service
  `expel-offline-subject` with reach swarm and reopens the follow whenever it
  ends, so it holds one across node3's restart. No node offers the service: a
  swarm follow carries to every member. The driver taps node1's and node2's
  logs until each logged a `services.discover` with node3.
- **while_down** `while_down.py` — with node3 stopped, the User expels node3 on
  node1. node1's direct push of the ban to node3 fails and is retried
  (`mod/user/src/push_until_delivered.go`); node2 receives the ban from the
  push to the local swarm, and the script waits until node2 lists it.
- **oracle** `verify.py` —
  - setup was live: node1 and node2 each logged a `services.discover` with
    node3 before the restart;
  - node3 restarted once; node1 and node2 hold the ban, issued by the User;
  - node3 reads itself `unclaimed` within 60 s of the oracle's start, and its
    log holds `leaving the swarm: this node is expelled` (leaveSwarm);
  - from 1 s after node3 reads unclaimed, for 15 s, node1 and node2 log no
    `services.discover` with node3 and reject no contract pushed by node3,
    while the follower still runs.

When node3 comes back it still holds its contract and links node1 and node2.
The ban reaches it at the first of these: the expulsion sync at a link with a
holder of the ban (`mod/user/src/object_receiver.go`), node1's retried push, or
the sweep (`mod/user/src/reconcile_task.go`). The receipt runs leaveSwarm, the
active contract is cleared, the user module emits `SwarmMemberLeft` for each
former member, and the services module drops their carries
(`mod/services/src/swarm.go` ReceiveObject, refreshMembers).

Ticket: https://wiki.satforge.dev/doc/Ta9lUBQ4V5 (A4G6 option b). Field report:
https://wiki.satforge.dev/doc/ebv4xLg2rP.

## What it does not judge

- Which path delivered the ban. The oracle's log of node1 lists node1's ban
  push lines it saw, for the report only.
- A carry node3 runs in the moments between its restart and the ban's arrival.
  Only what follows the leave is judged.
- A link a former sibling holds to node3 after the ban (expel-propagation
  reports it; refusing it is the link admission ticket's work).
