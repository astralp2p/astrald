# reconnect-sync-after-dead-link

A new link to a sibling makes a node ping its older links to that sibling
once; a dead older link closes within its ping timeout, and the close that
leaves the new link runs the sibling sync over it.

- **after** `adopt-node` (node1 and node2 linked siblings) · **destroys**
  `true` — node1 and node2 end with a link through a relay that drops every
  byte closed, and node1 holds an app no other test expects.
- **config** node1's and node2's tcp and kcp `link_idle_ping` 600 s and
  `link_ping_timeout` 3 s.
- **driver** `script.py` — runs `driver/` (Go, this repository's astral-go):
  - a TCP relay on the test host at node2's `host_ip` forwards to node1's
    `lan_endpoint`; node2 links to node1 through it (`nodes.new_link` with
    the relay's endpoint), and node1 closes every other link to node2
    (`nodes.close_link`), until both nodes hold the relayed link alone;
  - once the relay has forwarded no byte for 1 s (at most 30 s, else the
    driver fails), it starts dropping every byte in both directions and keeps
    both connections open, so each node holds a link that takes bytes and never
    answers;
  - an app registers on node1; its relay contract push to node2 goes into the
    dead link, and 17 s later, past the push's 15 s bound, `apphost.hosts` on
    node2 does not name node1 for the app;
  - node2 links to node1 at node1's `lan_endpoint`. The driver records how long
    node1 keeps the relayed link in `nodes.links` after that, and how long
    until `apphost.hosts` on node2 names node1 for the app.
  `script.py` records node1's log (`log.listen`) while `driver/` runs: the
  close of a link to node2 on its ping timeout, and every sibling sync with
  node2 that node1 starts.
- **oracle** `verify.py` — every step held, node1 dropped the relayed link
  within 10 s of the new link (the 3 s ping timeout plus slack), node1 started
  a sibling sync with node2 at or after that close, and `apphost.hosts` on
  node2 still names node1 for the app.

The 600 s idle interval keeps the keepalive from closing the dead link on its
own (tcp: 15 s + 10 s by default); otherwise node2's next link would count 1
on node1 and the first-link sync would deliver the app anyway.

Before the change, node2's direct link counted 2 on node1, so neither the
user receiver (`SyncNodesTask`) nor the nodes receiver (`UpdateNodeEndpoints`)
reacted, and nothing pinged the relayed link on the new link, which stayed pooled
for the whole idle interval. node2 never learned the app: its own sync toward node1
pushes only the relay contracts node2 holds. The probe is
`LinkPool.probeOlderLinks` (astrald `mod/nodes/src/link_pool.go`); the sync on
a close that leaves links is `onPartialLinkClose` in
`mod/user/src/object_receiver.go` and `mod/nodes/src/object_receiver.go`.

The sync the new link starts (`LinkCreatedEvent`, every link) waits on the
dead link and, once the probe closes it, also delivers the app over the new
link. The app alone therefore proves the probe, not the sync on the close; the
log check proves the sync on the close.

A query that times out with no response over a link also wakes that link's
ping (`onSilentTimeout`, `mod/nodes/src/mux.go`). A black hole that starts
while the syncs of the relayed link are in flight lets a push time out, and
the 3 s ping timeout then closes the link before node2 relinks, so node2 would
learn the app without the probe. The driver therefore waits until the relay has
been quiet for 1 s before it black-holes.

The relay stands in for a path that vanished without a reset, as a phone's old
Wi-Fi link does after the phone moves to mobile data. netsim `leave-lan`
(`tor.suite`) covers the same path over a real address change.

The 17 s wait counts from the start of the registration. `apphost.register`
pushes the relay contract at once, and the driver's registration returns only
once node1 has signed the app's contract for an Approval, seconds later.
Counted from that return, the check comes after the push's 15 s bound and the
3 s ping it wakes have closed the relayed link, and node2 has relinked and
learned the app without the probe.
