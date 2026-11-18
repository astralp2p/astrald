# services-consumer-restart

A consumer app's node restarts while the app follows a service, and the SDK's
reconnecting `Watcher` (astral-go `servicescli.WithReconnect`) reopens the
follow by itself: it keeps the set it held marked stale while the node is
down, and after node1 is back it lists a provider that started while node1
was down, with `Stale()` false, within 60 s.

- **after** `adopt-node` (node1 and node2 in one swarm) · **restart** `node1` ·
  **destroys** (every restarting test does) · the apps are processes on the
  test host that query their nodes; neither serves a listener.
- **driver** `script.py` — builds `driver/` (Go, this repository's astral-go)
  and starts two long-lived apps with `lib/apps.py`:
  - `driver provide` on node2 (provider A): registers an app holding
    `ServeApps` and keeps one open advertise binding of the service
    `services-consumer-restart-echo`.
  - `driver watch` on node1 (the consumer): registers an app, which node1's
    admin grants discovery of the service, and follows it with `reach=swarm`
    through `WatchIn(..., WithReconnect(0, 0))` (1 s doubling to 30 s). The
    app's state file holds the Watcher's offerings, `Stale()`, the count of
    `Synced()` signals and of follow ends (Stale turning true), the first
    boundary's outcome and `Err()`.
  The driver waits until the Watcher holds provider A after a boundary, not
  stale, records the counts, and exits; both apps keep running.
- **runner** — stops node1, runs `while_down.py`, starts node1 again. node2
  runs throughout.
- **while_down** `while_down.py` — the consumer's Watcher turned stale (its
  follow ended) and still holds provider A; then it starts `driver provide`
  on node2 as provider B and waits until node2 acknowledges B's binding. No
  follow open on node1 can see B.
- **oracle** `verify.py` — node1 restarted once and came back on its
  endpoint, node2 did not restart, and the three apps are the same live
  processes. Within 60 s the Watcher lists provider B and provider A after a
  further boundary, `Stale()` is false, `Err()` is empty, and the follow-end
  count grew.
- **recorded, not judged** — how long after node1's return the Watcher listed
  B; it depends on node1's boot, its link to node2 and the Watcher's backoff.

The consumer app opens no follow of its own after the restart: only the SDK
Watcher reopens it. Distinct from `services-node-restart`, which restarts the
provider's node under a follow that never ends.
