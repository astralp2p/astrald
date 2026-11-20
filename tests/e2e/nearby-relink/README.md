# nearby-relink

A swarm sibling that comes back to the LAN is relinked within seconds of being
heard, although the link task that maintains it waits out a backoff of
minutes: the user module wakes the task when nearby reports the sibling.

- **start** `nearby-silent`: `node1` and `node2` are linked siblings.
- **roster** `node1`, `node2`.
- **op** `lan-outage node2 --for 420`: `node2` leaves the LAN, every link to
  it is closed, and a timer in the guest brings it back at the same address
  420 s later.
- **attempts** each link attempt dials the peer's tcp and kcp rows; a kcp row
  gives up after the basic strategy's 5 s kcp budget
  (`mod/nodes/src/transport_profile.go` `transportProfiles`), so a failed
  attempt takes seconds and the backoff dominates the time between attempts.
- **driver** `script.py`: records both nodes' link-task and nearby-contact log
  lines (`log.listen`), and polls `node1`'s nearby list and links every second
  until the link has been back for 20 s.
- **oracle** `verify.py`: `node1` held no link at the start; `node2`'s entry
  left `node1`'s nearby cache and came back; `node1` listed the link within
  18 s of hearing `node2`. On `node1` or `node2`, the wake line is followed
  within 15 s by `restored after N attempts`, where the backoff after N
  failures is at least 60 s and the success came before any retry that
  backoff allows, counted from a `still trying to reconnect … (attempt M)`
  line, M < N, as if every attempt took no time.

Why 420 s: `node2`'s cache entry on `node1` expires at most 300 s after its
last status, and only then does nearby report it back at the same address.
With attempts of at most 5 s on each side, each side has failed nine
attempts by 300 s after the cut (the waits after failures 1 to 8 add up to
255 s) and waits 256 s for the tenth. The oracle's bound is then 480 s after the `attempt 5`
line, which falls 31 s or more after the cut: 511 s or later, against a return
420 s after the cut.

Either side may carry the proof: each maintains a link to the other, each
hears the other again within milliseconds, and whichever wakes first links;
the other side's wake then finds the link up and does nothing.
