# kcp-only-relink

A sibling that restarts while tcp between the siblings is dropped relinks
over kcp within 5 s of its link task starting: the basic strategy dials the
kcp rows that the restarted node holds for its sibling.

- **after** `adopt-node` (node1 and node2 linked siblings) · **roster**
  `node1`, `node2` · VM world.
- **ops** `block-tcp node1 node2` (every tcp packet between the pair is
  dropped and their tcp links are closed, so kcp over UDP is the only path);
  `restart-astrald node2 --in 30` (node2's astrald restarts 30 s later, while
  the driver watches).
- **driver** `script.py` — polls node2 every 0.2 s: whether it answers, and
  once it is back, its links to node1; taps node2's log as soon as it answers
  again, and node1's throughout. Judges nothing.
- **oracle** `verify.py` — node2 stopped answering and came back; node2
  lists a kcp link to node1; node2 linked within 5 s of its link task
  starting. The measure is node2's log, `starting to maintain link to
  <node1>` to `link to <node1> established`; when the tap opened too late for
  the first line, it is the host's polls, from node2 going down to the kcp link
  listed, which is longer.

why it fails at astrald `463a4fea`: basic was registered for tcp only, and
`BasicLinkStrategy.Signal` dropped every kcp row before a worker saw it, so
node2's link task ended in `link not produced` and backed off.

Not covered: node1 keeps the kcp link it held to node2's old process, since
kcp has no idle keepalive that sees it die.
