# swarm-expel-clean

While four siblings' apps talk, the User expels node4. The other three keep
answering each other, node4 and its app are unreachable in both directions,
and over a 30 s window no relay-contract push, swarm discovery or new link
crosses the ban once node4 answered it. This is flow F4 of the E2E tests
exhibit (https://wiki.satforge.dev/doc/e2e-tests-X3LHjItA2b).

- **after** `swarm-four-siblings` (node1-node4 in the User's swarm, every
  pair linked) · **destroys** `true` — a ban cannot be lifted ·
  **host_listener** — the four mesh apps serve their handlers on the test
  host.
- **driver** `script.py` — taps every node's log, spawns the `f4` mesh apps
  (`tests/e2e/internal/meshapp`, through `lib/meshapps.py`: period 1 s,
  `keep_removed`) on node1-node4 and waits up to 60 s until all 12 pairs
  answer. The User expels node4 on node1, and the flow holds 30 s. The
  driver lists each member's links to node4 at the end, stops the apps and
  records the `expel_clean` fact; the apps' state files and call logs stay.
- **oracle** `verify.py` —
  - setup: the 12 pairs answered before the expel, every tap is whole, every
    member logged the ban and node4 its own removal;
  - the six pairs among node1-node3 answered every call started after the
    expel;
  - from 1 s after the ban landed on the caller's node, every call to node4's
    app ended `route_not_found` or `rejected:<code>`; from 1 s after node4's
    removal, no call by node4's app answered;
  - every member's follow removed node4's offering within 15 s of the ban on
    its node, with cause `member_left`, or `member_lost` when the carry
    ended before the ban landed there, and never showed it again;
  - from 1 s after the later of the ban and node4's ban answer on each
    member: no `services.discover` naming node4 and no rejected contract
    pushed by node4; node1, the expelling node, logged node4's answer, since
    it pushes the ban to node4 until node4 answers; on node1, and on another
    member that logged the answer, no `added …-link with <id4>` after it and
    no link to node4 at the end; node4 logs no `services.discover` naming a
    member from 1 s after its removal.

Spec: `.ai/system/topics/app-routing.md` (Authorizing the relay, Failures),
`.ai/system/protocols/services/types/services.removed.md`,
`.ai/system/protocols/user/README.md` (link admission). Tickets:
https://wiki.satforge.dev/doc/qHhjbojGwM,
https://wiki.satforge.dev/doc/LIPwsObl5m,
https://wiki.satforge.dev/doc/CbMRLBQTFA,
https://wiki.satforge.dev/doc/Ta9lUBQ4V5,
https://wiki.satforge.dev/doc/cXSPe6unaV

## Spec conflicts

- The exhibit says node4's apps are "unreachable". The spec gives two
  outcomes: route not found when every host is a former member, a refusal
  when the host refused. The oracle accepts both.
- The exhibit says nothing relinks for the window. The spec admits the
  expelled node's links until it answered a push of its own ban from that
  node, so the no-link check starts at the answer.
- A follow's removal keeps the first cause. When node4 closes its links
  before the ban reaches a member, that member's carry ends first and the
  removal is `member_lost`; the oracle accepts it only before the ban landed
  there.
