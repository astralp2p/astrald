# blueprints-two-node

A type no SDK shipped is registered on node1, and an object of it crosses to a
reader at node2 — but only once that reader has pulled the schema over the link.

- **env** node · **start** `two-nodes` · **saves** —
- **driver** `script.py` — register a blueprint on node1, store one object of
  that type, and touch node2 not at all. The first registration tolerates an
  already-registered type so a reused world (`--keep`, `--target stage:`) does
  not report its own history as a flow failure.
- **oracle** `verify.py` — fail to read it, pull the schema from node1 through
  node2, read it again, and check the field values.

`objects.register_blueprint` adds a runtime type schema to a node and
`objects.get_blueprint` reads one back. The whole point is that a type the SDK
was never built against can still travel; nothing exercised it.

## The negative is the oracle's own state, not an inherited claim

The oracle is a fresh process, so its blueprint registry holds only what the
SDK ships and this type is not among them. It reads the object and fails, pulls the
schema, reads the same object again and succeeds — one process, one connection,
one difference. A test that took the driver's word for the first half would be
asserting the thing it is supposed to be measuring.

## What actually needs the schema

Not the node in the middle. node2's registry is checked before and after and
does not hold the type either time, while the object crosses and decodes
anyway.

This corrects the assumption the test document started from. node2 forwards
the query and carries the answer back as opaque bytes — `mod/nodes` selects a
link by target and pushes the response payload through untouched, and the
decode that needs the schema happens on node1, in `Module.Load`. So a node in
the middle never needs a schema for traffic passing through, and the thing
that must learn it is whoever decodes — here, the oracle process. The two-node shape still earns its place: it
is what makes "pulled over the link" a real crossing rather than a local
lookup, and it is what proves nothing replicates a blueprint between nodes.

## Settled while writing this

Registering a name twice is **refused**, not silently replaced —
`objects.register_blueprint: input 0: blueprint already registered`. The
document listed that as an open question. The check keys on the name before
any comparison of shape, so this is the general case and not just the
identical one; a registry that took the second definition would let any caller
redefine a live type's wire shape.

Worth knowing when reading that as a guarantee: astral-go's own comment treats
the same behaviour as a wart — "duplicate Register of an identical blueprint
fails instead of being idempotent" — and `blueprint_sync` treats
`ErrAlreadyRegistered` as success. The refusal is real; whether it is a
security property or an inconvenience is not this test's to settle.

Two questions the document raised remain open, both out of this test's start
state: whether a registration survives a restart (`env = "node"` has no restart
verb — see *Survive a node restart with state intact*), and whether the
unauthenticated squat deserves a test that passes today and fails when an
authorization hook lands.

## Shape

Two fields, `Label` (`string16`) and `Serial` (`uint64`), because one field
decodes correctly by luck more often than two do. The oracle checks the pulled
schema's field names and primitive types before trusting the decode, so a node
that answered a plausible but wrong schema fails on the shape rather than on a
mangled value.
