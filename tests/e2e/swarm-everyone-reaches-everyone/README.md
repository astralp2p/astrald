# swarm-everyone-reaches-everyone

Every node and every app of a four-node swarm reaches every other node and app
in each direction: node→node, node→app, app→node and app→app. A linked node of
another User reaches no app and no swarm read of it. This is F2 of the E2E
tests exhibit. It covers a node's own query to an app (fWdffmESP1,
tSct4beHzm), sibling-targeted queries (tVd1tG8js0, 0k6LsRMRBY), the caller
proof push of an app's query to a node (fd9pACrwVt) and the stranger guard
(IiDlv0Ksyz).

- **after** `swarm-four-siblings` (four siblings, every pair linked) ·
  **destroys** `true` — a node of a second User stays linked to node1.
- **host_listener**: each mesh app binds its handler on the test host at its
  node's `host_ip`.
- **driver** `script.py`:
  - `f2stranger` is claimed by a second User through a token-less
    `user.bootstrap` and links to node1 from its own side, as
    `swarm-authz-matrix`'s stranger does;
  - a mesh app (`lib/meshapps.py`, service `f2`, `period=0`) starts on node1
    to node4; each probes the other three node identities once with
    `apphost.whoami` and calls each provider its `reach=swarm` follow reports
    once. One more starts on `f2stranger`. The driver waits at most 60 s
    until every swarm app finished each first call and probe;
  - node→node: each node's token queries `user.info` on each other node;
  - node→app: each node's token calls `hello` on each remote swarm app;
  - the stranger's node token calls `hello` on each swarm app and
    `user.swarm_status` on node1, and a fresh app on the stranger
    (`meshapp call -as-app`) calls `hello` on each swarm app;
  - every query has a 15 s deadline; the driver stops the apps and records
    `everyone_reaches_everyone`: `{nodes, apps, states, node_node, node_app,
    stranger}`.
- **oracle** `verify.py`:
  - 48 swarm queries, 12 per kind, answer `ok` within 10 s;
  - node-token queries call as the node, and each `hello` reply names the
    calling node; each `apphost.whoami` names the calling app; a mesh app
    counts an app→app call `ok` only when the reply names it;
  - every stranger call to a swarm app ends `route_not_found` or rejected;
  - node1 refuses the stranger `user.swarm_status`;
  - the stranger's follow lists no offering, and each swarm app's follow
    lists exactly the three remote swarm apps;
  - no swarm app served the stranger's node or any of its apps.

The routing rules are astral-docs `topics/app-routing.md`: Resolving the host,
Sending to a node over its own link, Answering and Reach ("an `App` outside
the `Swarm` addresses none"). The stranger's reach is `topics/node-exposure.md`,
Linked peers, and its follow's is `protocols/services/ops/services.discover.md`,
Visibility. The 10 s bound is the umbrella's discovery-to-first-query bound
(operator decision D11).

The stranger's app→app calls come from a fresh app per call (`-as-app`), not
from its long-lived app's `-targets`: a `-targets` identity is recorded as
listed from the start, so the long-lived app's state could not show that its
follow listed no swarm offering.
