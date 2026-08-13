# fs-watch

A file dropped into a watched directory becomes an object node1 serves, under
the id its bytes hash to, and `objects.describe` places it at that path.

- **env** node · **start** `one-node` · **saves** —
- **driver** `script.py` — write a file, then `fs.new_watch` the directory
  holding it.
- **oracle** `verify.py` — poll for the object in the named repository, compare
  bytes, and read `mod.fs.file_location` back.

`fs.new_watch` registers a directory as an object repository, scans it in the
background and adds it to the `objects.RepoLocal` group. This is how a user's
own files enter the object graph — the one path by which astrald holds data
nobody typed into it — and no test touched `fs.new_repo`, `fs.new_watch`,
`objects.scan` or `objects.repositories`.

The mechanism works today, so this is a regression guard rather than a bug
hunt.

## The directory is inside the run's own tree

A test that registers a watch is telling a daemon to index a path, and the only
path it has any business indexing is one this run made. The watched directory
is created under the run's session directory and goes away with it.

## The file is written before the watch, on purpose

The initial background scan is what must find it. Writing afterwards would
exercise the fsnotify path instead, and the two are different mechanisms with
different failure modes — the second act belongs in its own test, along with
the `file_changed` transition.

## Timing is the whole difficulty

The watcher debounces per-file writes and the indexer hashes on a rate-limited
background queue, so indexing is eventual and an instant assertion is a flaky
one. The oracle polls to a deadline.

What it never does is poll for something the driver told it. The id is computed
here from the payload, so the question asked of node1 is "do you hold these
bytes", never "do you hold what your indexer decided to call them".

## Two raw-wire details

astral-py binds no `fs` module, so the watch is a raw query. The wire form is
`op?param=value` — the `-flag value` spelling the docs' `astral-query` examples
use fails as `route_not_found` rather than as a bad argument.

For the same reason `mod.fs.file_location` is a type the SDK was never built
against, and the descriptor would arrive undecodable. The oracle fetches the
schema from the node with `objects.learn` rather than reading the descriptor as
opaque bytes: a substring check against a raw payload would pass on a
descriptor that merely mentioned the path somewhere. A learned type arrives as
a `RuntimeRecord` whose fields carry their wire names, so they are read through
`.get("Path")` and `.get("NodeID")` rather than as Python attributes.

## Two claims

The repository serves the exact bytes, and the descriptor places them at a path
on this node. The second is what says a *file* was indexed rather than an
object arriving by some other route.
