# discover-swarm

node1, acting as the User, describes and searches an object that only node2
knows about. node2's answers reach node1 through the swarm describer and
searcher, and through explicit `sources`.

- **env** node · **start** `two-nodes-data-peer` · **saves** nothing
- **driver** `script.py` — give node2 a watched file, then on node1 call
  `objects.describe` and `objects.search` with `zone=dvn`, with
  `sources=<node2>`, with `sources=<node2>&sources_only=true`, and with
  `zone=dv`.
- **oracle** `verify.py` — repeats the calls and checks who answered.

node2 starts with nothing to describe or search for the object
`two-nodes-data-peer` stored there, so the driver writes a run-unique file into
a directory under the run's session tree and registers it with `fs.new_watch`
on node2. node2's fs describer then answers a `mod.fs.file_location` for it, and
its fs searcher matches the file name.

## What each call must return

| call on node1 | node2's answers | node1's own |
|---|---|---|
| `zone=dvn` | once | — |
| `zone=dvn&sources=<node2>` | twice: the swarm provider and the source | — |
| `zone=dvn&sources=<node2>&sources_only=true` | once | none |
| `zone=dv` | none | — |

`objects.search` drops repeated matches by object id, so its add-mode answer
holds node2's match once. Exactly one descriptor from node2 under `zone=dvn`
also shows the call does not loop: a query arriving over a link runs without
the network zone, so node2 never asks node1 back.

astral-py's `describe` and `search` pass extra keyword arguments to the call,
not to the query string, so the `sources` calls are raw queries.
`mod.fs.file_location` is learned from the node before a describe, as in
`fs-watch`.
