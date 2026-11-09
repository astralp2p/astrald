# app-describe-provider

A labeler app serves `objects.describe` for node1. The User asks node1 to
describe a note, and the app's label comes back with the app as its source.

- **roster** node1 · **after** `bootstrap-user-software-key` · `local_only`
- **driver** `script.py` — runs `driver/` (Go, this repository's astral-go) in
  `drive` mode:
  1. The User registers the labeler app with `apphost.register`, asking for
     `mod.auth.serve_objects_action` limited to the `describer` role.
  2. The app serves through `apps.ServeWith` with `apps.WithObjectDescriber`,
     which mounts `objects.describe` and registers the app with
     `objects.register_describer`. Its describer labels one test-unique note
     with a `String16` label.
  3. The User runs `objects.describe` for the note on node1. The answer is the
     label alone, its `SourceID` is the app, and the app was asked once.
  4. The User revokes the app's grant with `apphost.revoke`. The same describe
     answers nothing, and the app is not asked again.
- **oracle** `verify.py` — every step held, plus the same story with a fresh
  app and a fresh note (`driver verify`).

## How node1 reaches the app

`objects.register_describer` adds the caller as an external describer under a
lease (`mod/objects/src/external_registration.go`). `objects.describe` fans the
request out to every registered describer, and an external describer queries
`objects.describe` on the app's identity and stamps each descriptor with that
identity (`mod/objects/src/external_describer.go`). The node rechecks the
describer grant on every fan-out and drops a describer that no longer holds it,
so the revoke in step 4 also leaves no describer registered in the shared
world.

`registration-lease` covers the lease of a describer registration, with
registrants that serve nothing. This test covers an app that serves the
descriptor the User receives.

`apps.WithObjectDescriber` registers through the process-default objects
client, so the driver makes the app's client the process default and gives the
User a client of its own. The note is never stored: `objects.describe` asks the
describers whether or not node1 holds the object.
