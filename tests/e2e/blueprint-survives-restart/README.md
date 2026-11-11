# blueprint-survives-restart

An app with types of its own syncs their blueprints to node1, node1 restarts,
and the reconnecting app syncs them again: node1 lists the types, describes
them as declared, and decodes an object of one of them again.

- **after** `bootstrap-user-software-key` (node1 with its User) · **restart**
  `node1` · **destroys** (every restarting test does) · **local_only** — only a
  local world restarts a node, and the app is a process on the host.
- **driver** `script.py` — builds `driver/` (Go, this repository's astral-go)
  and starts `driver serve` as a long-lived app with `lib/apps.py`. The app
  registers on node1 (`apphost.register`), adds `Note` (`Label string16`,
  `Serial uint64`) and `Tag` (`Name string16`, `Rank uint8`) to its registry,
  and serves through `apps.ServeWith` with `apps.WithBlueprintSync()` followed by
  a hook that counts completed syncs. Once the first sync is counted, the
  driver, as the User, records what node1 holds: both names in
  `objects.blueprints`, both schemas from `objects.get_blueprint`, and an object
  of `Note` stored in `local` and read back after `objects.learn`. As a control
  it also registers a blueprint itself (`blueprint_survives_restart.control`)
  that no app syncs. The driver then exits; the app keeps running.
- **runner** — restarts node1 between driver and oracle, on the endpoint the app
  dials (`lib/worlds/local.py` keeps a restarted node's ports).
- **oracle** `verify.py` — the facts above held before the restart; node1
  restarted once; the app is the same pid and alive; the app's own state shows
  another completed sync (`syncs` grew); node1 lists both types again within
  90 s, describes them as declared, and the object stored before the restart:
  a fresh process cannot read it (`blueprint not found`, naming the type) until
  it learns the type from node1, and then decodes the stored label and serial.

## Does node1 keep blueprints over a restart?

Not judged, recorded in the oracle's log. Runtime blueprints live in the node's
in-memory registry (`astral.DefaultBlueprints()`, `mod/objects/src/module.go`),
and no code persisting them is known. The control type answers it without an app
in the way: the User registers it before the restart and no app syncs it. This
build lists it after the restart or not, and the note says which. The app's own
types are a clean sample only when the app had not yet synced again at the first
look, which the note also says; the registrar reconnects within a second or
two, so the sample is clean in some runs and not in others, and the control is
the measurement. Measured on this host: the control is not listed after the
restart, and in a run whose sample was clean the app's types were not listed
either until its second sync. That is the
open question of the ticket "Run blueprint sync first and fail on a changed
ObjectID".

## Why the object, not a typed reply

A node relays an app's reply as bytes, and the reader's process decodes it, so a
reply says nothing about the node's registry. A reader that learns the schema
from the node (`objects.learn` asks `objects.get_blueprint`) does depend on it,
which is what the stored object exercises: the node keeps the object and forgets
the type, and the type returns only through the app's sync.

## The long-lived app

See `tests/README.md`, Long-lived apps. The oracle stops the app when it
finishes; the runner stops it at the world's close if the oracle never got to.
Names are world-wide, so the app is `blueprint-survives-restart-app` and the
types are `blueprint_survives_restart.*`.
