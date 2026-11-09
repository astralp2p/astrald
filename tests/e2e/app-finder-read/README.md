# app-finder-read

A notes app on node2 holds a note that no repository holds. The app serves
`objects.find` and `objects.read` for the note and registers as a finder. A
read on node2, and the User's read on node1, both come back with the note's
bytes, served by the app.

- **env** node · **start** `two-nodes` · **saves** —
- **driver** `script.py` — runs `driver/` (Go, this repository's astral-go) in
  `drive` mode:
  1. an app registered without `mod.auth.serve_objects_action` is refused by
     `objects.register_finder` with `not permitted`;
  2. the notes app, registered on node2 with `mod.auth.serve_objects_action`,
     serves `objects.find` and `objects.read` through `apps.ServeWith` with its
     own client, and registers as a finder from a registration hook;
  3. `objects.find` on node2 names the notes app alone;
  4. `objects.read` on node2, with no repository, returns the note's bytes;
  5. the User's `objects.read` on node1, targeted at node2, returns the same
     bytes;
  6. the notes app served both reads, each to node2 as caller.
- **oracle** `verify.py` — every step held, plus the same flow with a fresh
  app and note (`driver verify`).

A read with no repository reads from the candidates the finders name. A
candidate other than the node receives an `objects.read` from the node, as the
node, with `zone=dv` (`mod/objects/src/op_read.go`). An app finder that names
itself therefore makes the app the source of the bytes.

An app registers as a finder only on the node that hosts it, and only that
node's finders see it. node1's own swarm finder asks node2's `main` repository
alone (`mod/user/src/object_finder.go`), so the User's read on node1 reaches
the notes app by targeting node2. node2 answers a query from the network from
its own finders, the notes app among them.

The app asks for a two-minute finder lease. The lease outlasts the flow and
lapses soon after it, so no later test in the world meets the app in node2's
fan-out.
