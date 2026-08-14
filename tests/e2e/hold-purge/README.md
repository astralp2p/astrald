# hold-purge

A held object survives `objects.purge`; an unheld one does not.

- **env** node · **start** `two-nodes` · **saves** — · **mutates** true
- **driver** `script.py` — store two stamped objects, load both, hold the first
  with no duration, then purge the `local` repo.
- **oracle** `verify.py` — the held object is still there and re-encodes to its
  own id; the unheld one is gone.

A hold that is recorded and not consulted destroys an app's data on the next
purge, and nothing in the repository would notice. Neither op had any coverage:
no test in the tree called `apphost.hold_object` or `objects.purge`.

## The hold is what saves it — verified by negative control

With the single `apphost.hold_object` call removed and everything else
identical, **both** objects are purged. That is what rules out the failure this
test exists to avoid: the survivor is not surviving because it was never
purge-eligible, nor from read ordering, nor from the purge stopping early — the
same call freed its sibling.

## Stamped objects, not raw bytes — this is load-bearing

Purge walks the objects module's tracking table, and a row is seeded only for a
payload the module recognises as an astral object:

- `Module.Store` seeds a typed store — `mod/objects/src/op_store.go:32`,
  `module.go:125`.
- `objects.create` seeds too, when the committed bytes carry a valid stamp —
  `op_create.go:71` via `module.go:194-201`.

The distinction is the **stamp, not the op**: canonical bytes written through
`create` + commit are purge-eligible, and unstamped bytes are not, whichever
path wrote them. astrald says so in its own source:

    // note: a blob reached only via Load is not seeded; objects without a
    // tracking row are not purge-eligible. accepted limitation for now.
    — mod/objects/src/module.go:100-101

The first draft of this test used raw text blobs, the way `object-store` does,
and the purge freed **0 objects**. That is the trap worth naming: with raw
bytes the test would have been green for the wrong reason on a node whose holds
were broken, because nothing would be purged and the hold would never be
consulted at all.

`astral.Query` is the payload type for want of a plainer one — a core
registered record with a free-text field, so two of them with different text
are two distinct objects. Nothing about the test depends on what it means.

That unstamped blobs are permanently unreclaimable is tracked separately as
*astrald: an unstamped blob is never purge-eligible, so a node cannot reclaim
one*.

## Why `mutates`, and what it does not buy

`objects.purge` deletes every unheld object in the repository, so this test
invalidates any state whose value includes stored objects. `mutates = true`
with no `saves` is TERMINAL: the harness refuses to let any test **follow** it
in a suite walk (`lib/manifest.py:189-192`).

That is narrower than it sounds. It does not stop `object-store` running
*before* this test — `validate_order(['object-store', 'hold-purge'])` is
accepted — it guarantees that nothing downstream can consume the data this test
destroys. Which is the property that matters, and is why the test is in no
suite: `main.suite` rejects it at either end, after `expel-node` by that test's
`two-nodes-expel` fence and before it by this test's TERMINAL one.

## Two halves, both required

"The held object is still there" alone passes on a node whose purge does
nothing at all — the same silent failure as a purge that ignores holds. One
destroys data, the other never reclaims any, and only checking both tells them
apart.

The verdict comes from the repository, never from the purge's own list of what
it freed. That list is carried as a fact for the report; a purge that returns a
convincing list and deletes the wrong thing is exactly what a test trusting it
would miss.

## What it does not cover

Only the permanent hold. An expiring hold (`duration` non-nil) and
`unhold_object` are untouched.

The hold is *recorded* per caller (`mod/apphost/src/op_hold_object.go:39`) but
*consulted* identity-blind — `db.ObjectHeld` filters on object and expiry with
no `app_id` predicate — so a hold placed by any identity protects the object.
This test cannot tell one holder from another and does not claim to.

No `agent` driver. The oracle needs the objects to carry a stamp, and the
operator flow behind "store a short text" is `create` + `write` + `commit` of
raw bytes, which purge cannot free. An operator following the obvious reading
would land a red recorded as `failure_kind: verify` — "astrald misbehaved" —
for a prompt that was never precise enough. Better no agent driver than one
that blames the daemon for the prompt.
