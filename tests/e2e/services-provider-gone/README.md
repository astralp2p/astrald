# services-provider-gone

A provider app on node2 stops cleanly, crashes and restarts, answers late and
hangs; a `services.discover` follow on node1 removes it each time it goes and
shows it again each time it comes back.

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
     removed (`final_stop_removes_the_provider`);
  7. a provider process that answers its next 2 asks 12 s late (past node2's
     10 s request timeout) is shown (`slow_provider_shown`); node2's probes of
     the followed binding (every 60 s) go unanswered twice, node2 retires the
     binding and the follow removes it within 170 s
     (`slow_provider_is_retired`); the provider's keeper advertises again and
     the follow shows it within 30 s (`slow_provider_advertises_again`);
  8. the provider gets SIGSTOP, so it keeps its advertise channel open but
     answers nothing; the follow removes it within 150 s
     (`hung_provider_is_retired`); after SIGCONT its keeper advertises again
     and the follow shows it within 30 s
     (`resumed_provider_advertises_again`);
  9. the provider stops cleanly and is removed
     (`liveness_stop_removes_the_provider`), so no provider outlives the test.
  Every removal carries cause `member_removed`: node2 ends the provider's
  binding and sends the removal, which node1 forwards.
- **oracle** `verify.py` — every step held, plus steps 1 to 6 again with fresh
  identities (`driver verify`). Steps 7 to 9 take about 5 minutes with the
  node's default liveness timings, so the oracle reads them from the driver's
  facts only.

The test judges discovery updates only. It never calls the provider, so the
first-connection `route_not_found` race (relay contract pushed on its own
goroutine; see `app-discover-follow`) does not enter. The service name is
unique to this test, and no provider or binding is left behind.

Not covered: a second advertisement of a name already in use (`ErrOwned`) and
reaching the provider after its loss.

The flow is in Go because astral-py speaks the pre-v1 Services wire.
Spec: `.ai/system/protocols/services/ops/services.discover.md`,
`.ai/system/protocols/services/ops/services.advertise.md#liveness`.
