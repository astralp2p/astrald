# app-ping-pong

A player app on node1 and a player app on node2 each serve `ping`, and each
pings the other by its app identity across the link. Each player answers
`pong, <caller>`, and each answer names the player that pinged. Each player
has its own identity, token and client, and uses that one client both to
serve and to call.

- **env** node · **start** `two-nodes` · **saves** —
- **driver** `script.py` — runs `driver/` (Go, this repository's astral-go) in
  `drive` mode:
  - node1 registers player1 and node2 registers player2 (`apphost.register`);
    each player serves `ping` through `apps.ServeWith` on a client of its own
    token;
  - `apphost.hosts` on node1 names node2 as player2's host, and
    `apphost.hosts` on node2 names node1 as player1's host;
  - player1 pings player2 once, and player2 pings player1 once; each answer
    is `pong, <the pinging player>`.
- **oracle** `verify.py` — every step held, plus the same story with fresh
  identities (`driver verify`).

A node routes a query for an app on its sibling through the app's relay
contract (`mod/apphost/src/query_preprocessor.go`). `apphost.register` pushes
that contract to the linked siblings in a background goroutine
(`mod/apphost/src/op_register.go`, `PushToSiblings` in `mod/user/src/sync.go`),
so a call made as soon as a player registers can race the push and fail with
`route_not_found`. The driver waits, within 30 s per direction, until
`apphost.hosts` on each node names the other node for the other player, and
then pings once in each direction, with no retry. The wait is the workaround
for the `route_not_found` race
(https://wiki.satforge.dev/doc/swarm-discovery-first-connection-to-a-new-provider-fails-with-route_not_found-taD5d1d8Zn)
until push-before-emit lands.

The serving node sees the pinging player as the caller because the calling
node attaches that player's own relay contract to the query
(`query_preprocessor.go`).

The driver pings once both players' registration hooks have run: the
registrar reports `Ready` as soon as it binds, before it attaches a handler,
and a hook runs only after the attach (astral-go `lib/apps/registrar.go`).

`app-call-remote` calls from node1 to node2 alone; this test adds the call from
node2 to node1, made by an app that also serves.

The players' handlers end with the driver, as `apphost.bind` removes them when
its session closes. The players' identities, tokens and relay contracts stay
on node1 and node2.
