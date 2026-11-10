# services-provider-gone

A provider app on node2 stops cleanly, crashes and restarts; a
`services.discover` follow on node1 removes it each time it goes and shows it
again each time it comes back.

- **env** node · **start** `two-nodes` · **saves** —
- **driver** `script.py` — runs `driver/` (Go, this repository's pinned
  astral-go) in `drive` mode. The provider runs as a child process of the
  driver (the same binary, started through `os.Executable()`), so it can be
  killed. It serves under one app identity that the driver registers on
  node2 and hands to each incarnation by token:
  1. a consumer app on node1, holding a discovery grant for `spg-echo`, opens a
     `services.discover` follow with `reach=swarm`; the first event is a
     complete initial outcome with no offering (`follow_starts_empty`);
  2. the provider process starts; the follow shows its available offering of
     `spg-echo` (`follow_shows_the_provider`);
  3. the provider gets SIGTERM and closes its advertisement; the follow removes
     exactly that offering (`clean_stop_removes_the_provider`), and a one-shot
     swarm discovery from the consumer completes with no provider
     (`clean_stop_leaves_no_provider`);
  4. the provider restarts under the same identity; the follow shows it again
     (`restart_shows_the_provider_again`);
  5. the provider is killed with SIGKILL, with no cleanup; the node sees its
     session drop and the follow removes the offering
     (`crash_removes_the_provider`, `crash_leaves_no_provider`);
  6. the provider restarts once more and is shown again
     (`restart_after_crash_shows_the_provider`), then stops cleanly and is
     removed (`final_stop_removes_the_provider`), so no provider outlives the
     test.
- **oracle** `verify.py` — every step held, plus the whole story again with
  fresh identities (`driver verify`).

The test judges discovery updates only. It never calls the provider, so the
first-connection `route_not_found` race (relay contract pushed on its own
goroutine; see `app-discover-follow`) does not enter. The service name is
unique to this test, and no provider or binding is left behind.

Not covered: a second advertisement of a name already in use (`ErrOwned`) and
reaching the provider after its loss.

The flow is in Go because astral-py speaks the pre-v1 Services wire.
Spec: `.ai/system/protocols/services/ops/services.discover.md`.
