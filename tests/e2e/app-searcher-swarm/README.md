# app-searcher-swarm

A recipes app on node2 registers as a searcher. The User searches the swarm
from node1 (`zone=dvn`), and the search reaches the app on node2 exactly once
and comes back with the app's recipe, the app named as its source.

- **env** node · **start** `two-nodes` · **saves** —
- **driver** `script.py` — runs `driver/` (Go, this repository's astral-go) in
  `drive` mode:
  1. an app registered without `mod.auth.serve_objects_action` is refused by
     `objects.register_searcher` with `not permitted`;
  2. the recipes app, registered on node2 with
     `mod.auth.serve_objects_action`, serves `objects.search` through
     `apps.ServeWith` with its own client, and registers as a searcher from a
     registration hook;
  3. the User's `objects.search` on node1 with `zone=dvn`, for a word only the
     recipe matches, answers the recipe alone;
  4. the match names the recipes app as its `SourceID`;
  5. the recipes app was asked once, with the User's words, by node2.
- **oracle** `verify.py` — every step held, plus the same flow with a fresh
  app and recipe (`driver verify`).

node1's swarm searcher asks every other swarm member to search, as node1
(`mod/user/src/object_searcher.go`). node2 receives the query from the network,
drops the `network` zone, and runs its own searchers alone, so the search never
returns to node1 and the app is asked once. node2 asks the app as itself and
stamps each of the app's results with the app's identity as `SourceID`
(`mod/objects/src/external_searcher.go`). node1 passes the stamped result on
unchanged.

The app asks for a two-minute searcher lease. The lease outlasts the flow and
lapses soon after it, so no later test in the world meets the app in node2's
fan-out.
