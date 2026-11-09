# app-search-provider

A catalog app serves `objects.search` for node1. The User searches node1, and
the app's result comes back with the app as its source.

- **roster** node1 · **after** `bootstrap-user-software-key` · `local_only`
- **driver** `script.py` — runs `driver/` (Go, this repository's astral-go) in
  `drive` mode:
  1. The User registers the catalog app with `apphost.register`, asking for
     `mod.auth.serve_objects_action` limited to the `searcher` role.
  2. The app serves through `apps.ServeWith` with `apps.WithObjectSearcher`,
     which mounts `objects.search` and registers the app with
     `objects.register_searcher`. Its searcher answers one test-unique word
     with one object ID, the ID of a note it knows.
  3. The User runs `objects.search` for the word on node1. The answer is the
     note alone, its `SourceID` is the app, and the app was asked once.
  4. The User revokes the app's grant with `apphost.revoke`. The same search
     answers nothing, and the app is not asked again.
- **oracle** `verify.py` — every step held, plus the same story with a fresh
  app and a fresh word (`driver verify`).

## How node1 reaches the app

`objects.register_searcher` adds the caller as an external searcher under a
lease (`mod/objects/src/external_registration.go`). `objects.search` fans the
query out to every registered searcher, and an external searcher queries
`objects.search` on the app's identity and stamps each result with that
identity (`mod/objects/src/external_searcher.go`). The node rechecks the
searcher grant on every fan-out and drops a searcher that no longer holds it,
so the revoke in step 4 also leaves no searcher registered in the shared world.

`apps.WithObjectSearcher` registers through the process-default objects client,
so the driver makes the app's client the process default and gives the User a
client of its own. The object ID is never stored: a searcher answers with IDs,
and `objects.search` without `repo` relays them as they are.
