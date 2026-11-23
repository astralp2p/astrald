# sibling-query-live-link

Queries that a node, its User or its sibling send to the sibling's node
identity over a live link answer, in both directions, across a new link and
the close of an older one. None answers `route_not_found`. This is M4 of the
E2E tests exhibit, the local half of the report "sibling-targeted queries end
route_not_found over a live link" (tVd1tG8js0, AC1) and of its diagnosis
ticket 0k6LsRMRBY. The netsim reproduction stays with that ticket.

- **after** `adopt-node` (node1 and node2 in the User's swarm, linked).
- **destroys** nothing: the new link it opens and the older link it closes
  leave one link between node1 and node2, as `adopt-node` left them.
- **driver** `script.py` (astral-py), 20 rounds, 0.5 s apart:
  - before each round it reads both nodes' links to each other;
  - each round sends five queries, each with a 5 s deadline: node1's node
    token to `node2:user.info` and `node2:objects.contains` (an absent
    object), the User's token on node1 to `node2:user.info`, and node2's node
    token to `node1:user.info` and `node1:objects.contains`;
  - before round 10 the User on node1 opens a second link to node2's
    `lan_endpoint` (`nodes.new_link`);
  - before round 15 node1 closes every link to node2 it held before the new
    one (`nodes.close_link`);
  - it records `sibling_query_live_link`: `{rounds, opened, closed,
    links_end}`.
- **oracle** `verify.py`:
  - the second link opened, and from round 15 on node1 lists none of the
    older links;
  - before every round both nodes list a link to each other;
  - 100 of 100 queries answer: `user.info` an `Info` whose node is the queried
    node, `objects.contains` false;
  - no query ends `route_not_found`, and each takes at most 2 s.

A link carries queries between its two parties (astral-docs
`core-definitions/link.md`; `topics/link-mux.md`, Sessions). Route not found
means no node on the path routes the query (`core-definitions/query.md`,
Routing outcomes), so a query to a node over a live link never ends with it.
A sibling and the User reach `user.info` and `objects.contains`
(`swarm-authz-matrix`, The contract). A link `nodes.new_link` dials to an
explicit endpoint skips the strategy settlement, the only step that prunes
duplicate links (`mod/nodes/src/create_link_task.go`;
`mod/nodes/src/link_duplicates.go`, `offerStrategyLink`), so both links stay
up until node1 closes the older one. An older link a node closed by
itself before round 15 is recorded in `closed.gone_before`.

The 2 s bound is derived: a loopback round trip with wide slack. No source
bounds the answer time of these ops.
