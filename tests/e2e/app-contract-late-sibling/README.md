# app-contract-late-sibling

An app registered on node1 while node2 is down is reachable from an app on
node2 on that app's first query after node2 returns. This is M2 of the E2E
tests exhibit. It covers the pull on a miss (tyUYbuZqVI, jw9TUNNdUH) and the
registration push retry (qArE8Rfm7T), whose push names only the siblings
linked at the registration.

- **after** `adopt-node` (node1 and node2 in the User's swarm, linked) ·
  **restart** `node2` · **destroys** `true` — a provider app stays registered
  on node1 and node2 restarted.
- **host_listener**: the provider app binds its handler on the test host at
  node1's `host_ip`.
- **driver** `script.py` — records `late_sibling`: `{node1_restarts_before,
  node2_restarts_before, node2_endpoint, driver_end}`. It judges nothing.
- **while_down** `while_down.py` — while node2 is stopped, a mesh app
  (`lib/meshapps.py`, service `m2-late`) registers on node1 and advertises;
  the script then waits 12 s, past the 10 s first push retry
  (`mod/user/src/push_siblings_retry.go`). It writes no facts.
- **oracle** `verify.py`:
  - node2 restarted once, node1 not at all, and node2 came back on its
    endpoint;
  - the provider is alive and registered after the driver ended;
  - node2 lists a link to node1 within 30 s;
  - one query, no retry, from a fresh app on node2 (`meshapp call -as-app`)
    to the provider's `hello` answers `ok` within 5 s, and the reply names
    the calling app;
  - node2's `apphost.hosts` for the provider, read before the call, is
    printed as the path evidence and not judged;
  - the oracle stops the provider.

A member that is not linked when an app registers receives the app's contract
at its next first link to a member that holds it, and a member that missed
both deliveries obtains it on its first query to the app, by asking its linked
members (astral-docs `topics/app-routing.md`, Reach; Resolving the host). The
5 s bound is the 3 s wait for that pull plus a relayed round trip on loopback.
Either path passes; the oracle judges the first query, not the path.
