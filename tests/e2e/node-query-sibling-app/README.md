# node-query-sibling-app

A node's own query to an app on its sibling answers and names the node as the
caller. It never hangs. This is M3 of the E2E tests exhibit, the repro of the
feedback report "a node's own query to a sibling app hangs" (fWdffmESP1) and
the check of its fix, a node's own query sent as a relay frame (tSct4beHzm).

- **after** `adopt-node` (node1 and node2 in the User's swarm, linked).
- **destroys** nothing: the test adds two app contracts and changes nothing a
  later test reads, as `app-call-remote` does.
- **host_listener**: each mesh app binds its handler on the test host at its
  node's `host_ip`.
- **driver** `script.py`:
  - starts a mesh app (`tests/e2e/internal/meshapp`, `lib/meshapps.py`) on
    node1 and on node2, service `m3`, and waits until both registered;
  - three times each: node1's node token calls node2's app `hello`, and
    node2's node token calls node1's app `hello`, each with a 15 s deadline
    (`meshapp call`);
  - stops the apps and records `node_query_sibling_app`:
    `{apps, calls, states}`.
- **oracle** `verify.py`:
  - 6 of 6 calls answer `ok` within 5 s;
  - each reply is `hello, <calling node>`, and the call's own identity is the
    calling node;
  - each app counted the remote node as a caller exactly 3 times.

The two apps also follow `m3` with `reach=swarm` and call each other once.
Those calls are counted under the apps' identities and are not judged.

The caller rule is astral-docs `topics/app-routing.md`: a relay frame names
the original caller (Resolving the host), and a relayed query whose caller is
the relaying node itself is not a relay and is not checked (Authorizing the
relay). The 5 s bound is the 3 s pull wait of Resolving the host plus a
relayed round trip on loopback.
