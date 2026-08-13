# read-refusal

A stranger — an identity that is neither the User nor a node of the local
swarm — is refused every op that hands out or destroys object bytes, while
the User still reads the same object in full.

- **env** node · **start** `two-nodes-data` · **saves** —
- **driver** `script.py` — `apphost.register` on node1 mints the stranger, and
  the User stores a decoy for the delete probe.
- **oracle** `verify.py` — the stranger is probed against `objects.read`,
  `objects.load`, `objects.contains` and `objects.delete`; all four must
  refuse, and the User's own read must still return the object.

## This test is red on master, on purpose

At `d43def01` only `objects.read` refuses the stranger. `objects.load` and
`objects.contains` serve it, and `objects.delete` deletes the decoy:

    a stranger — neither the User nor a swarm node — was served by
    objects.load, objects.contains, objects.delete (the decoy is gone);
    only 1 of 4 refused it

That is the defect the test was written to hold, tracked as *astrald:
objects.load, contains and delete bypass the read authorizer*. The test lands
red by operator decision; it goes green when the fix does, and it is **not in
`main.suite`** until then, so the everyday chain keeps saying what it means.

## Why it is shaped this way

The stranger must be a real distinct identity. An anonymous local guest is
rewritten to the node's own identity by the core router, and the node is a
swarm member, so an anonymous connection would test the granted path under
another name.

The delete probe aims at a decoy, never at the state's own object. A stranger
that can delete would otherwise destroy `two-nodes-data` on its way to proving
it should not have been able to — and the test would take the rest of the
chain down with it.

All four probes run before anything is asserted, so the failure names every op
that leaked rather than the first one.

Delete is judged by its effect: the decoy is still there afterwards, or it is
not. An op that answers `ack` and does nothing and an op that refuses outright
are the same verdict here, because the bytes are what survive or do not.

No `payload.txt`: the object id is a content hash, so the User's read proves
itself against the id it asked for.
