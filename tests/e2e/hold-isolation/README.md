# hold-isolation

A hold belongs to the app that placed it. It lasts as long as that app's
longest hold on the object, and `objects.purge` keeps exactly the objects
some app still holds.

- **after** `bootstrap-user-software-key` · **destroys** true (it purges
  node1's `local` repository) · **roster** node1
- **driver** `script.py`: as the User, store four typed objects X, Y, Z and W,
  and register apps A and B with `mod.auth.store_objects_action` and app N
  with no permits. Then:
  - A and B hold X, then A unholds X.
  - A holds Y for 2 s.
  - A holds Z indefinitely, then again for 1 s.
  - N tries to hold W.
  - B unholds Z, which B never held.
  - After 3 s, list each app's holds, then purge `local` as the User.
- **oracle** `verify.py`:
  - X and Z are still in `local`, and each re-encodes to its own id.
  - Y and W are gone.
  - Listed with each app's own token, A holds exactly [Z], B holds exactly [X]
    and N holds nothing.
  - N's hold was answered `not permitted`, and B's unhold was answered `ack`.

hold-purge covers one app with one indefinite hold. This test adds what
hold-purge does not cover: holds of two apps on one object, expiry, the
merge of two holds on one object, the permit gate and `unhold_object`.

## What each object proves

| object | flow | broken behaviour that turns it red |
|--------|------|-----------------------------------|
| X | A and B hold, A unholds | unhold deletes every app's hold, not only the caller's; X is purged |
| Y | A holds for 2 s | the expiry is ignored; Y survives, and A still lists it |
| Z | A holds indefinitely, then for 1 s | the shorter re-hold replaces the indefinite one; the hold lapses and Z is purged |
| Z | B unholds a hold it does not have | B's unhold deletes A's hold; Z is purged and A no longer lists it |
| W | N, with no permit, holds | the permit gate is skipped; W survives and N lists it |

Sources:

- `HoldObject` in `mod/apphost/src/db.go`: a re-hold is merged with
  `CASE … IS NULL … THEN NULL ELSE MAX(…)`, so an indefinite hold always wins.
- `UnholdObject` in the same file: it deletes only rows whose `app_id` is the
  caller.
- `op_hold_object.go:36`: the permit check.
- astral-docs `protocols/apphost/ops/apphost.hold_object.md`,
  `apphost.unhold_object.md` ("the calling app's hold") and
  `apphost.list_held_objects.md`.

Purge itself checks holds without looking at which app placed them
(`ObjectHeld` filters on the object and the expiry only). So a hold is tied
to its app only by `unhold_object` and `list_held_objects`, and the test
checks both.

## Survivors and purged objects are both required

A purge that frees nothing would leave X and Z in place without any hold
being checked. Y and W gone shows that this purge does free unheld objects.
Y also has to appear in A's list right after the holds; without that, Y
being gone would look the same whether its hold expired or was never placed.

The payloads are typed `astral.Query` records, because purge frees only
objects the objects module tracks (see hold-purge's README). With raw blobs
nothing would be purged, and every check above would pass for the wrong
reason.

## What the oracle trusts

The oracle checks the repository and connects as each app to list its holds.
It trusts the driver for two of the node's answers: N's `not permitted` and
B's `ack`. The oracle cannot ask the node for either answer afterwards, so
each has a second check: W purged and N holding nothing for the first, and Z
surviving with A still holding it for the second. The purge's list of freed
ids is only reported.

## Timing

Y's 2 s expiry and the 3 s wait come from a live run. The wait starts after
Z's re-hold, so Y has been held at least 3 s when the purge runs, and Z's
1 s re-hold would have lapsed. A run takes milliseconds per op on a local or
Docker node. If a run flakes, widen `WAIT` in `script.py`, and keep every
assertion.

## What it does not cover

- Batch-mode hold and unhold (no `id`, ids streamed in).
- Holds placed under a `Partial Object ID`.
- A hold on an object the node has not fetched yet.
- No `agent` driver, for the reason hold-purge gives: an operator who stores
  text stores untyped bytes, and purge cannot free those.
