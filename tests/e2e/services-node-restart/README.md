# services-node-restart

A provider app's node restarts, and a follow of its service on another node
removes the offering and then shows it again: the provider app re-registers
with the token it holds and advertises again, and the follow, which never
ended, receives the same provider's offering available once more.

- **after** `adopt-node` (node1 and node2 in one swarm) · **restart** `node2` ·
  **destroys** (every restarting test does) · **host_listener** — both apps
  are processes on the test host; the provider binds at node2's `host_ip`.
- **driver** `script.py` — builds `driver/` (Go, this repository's astral-go)
  and starts two long-lived apps with `lib/apps.py`:
  - `driver provide` on node2: registers a provider app holding `ServeApps`
    and serves the op `services_node_restart_echo` through the SDK's
    `AppRegistrar`, which keeps the bind session and re-attaches the handler
    whenever the session ends. A registration hook advertises the service
    `services-node-restart-echo` (open) on every attach, and the app's state
    file records the attach count, the advertise count and every ask the node
    sent it to evaluate the offering.
  - `driver follow` on node1: registers a consumer app, which node1's admin
    grants discovery of the service, and follows it with `reach=swarm`. Every
    event of the stream (an update, a Removed, the initial outcome, an error)
    goes into the app's state file, in order, with the offerings it holds.
  The driver waits until the follow holds the provider, calls the provider once
  from a fresh app on node1 (`driver call`), and records the apps' identities
  and pids, node2's restart count and endpoint, the provider's attach and
  advertise counts and how many events the follow had. It then exits; both
  apps keep running.
- **runner** — restarts node2 between driver and oracle. node1 is not
  restarted, so the follow's stream on node1 is one stream across the whole
  story. node2 comes back on its leased endpoint with its token table and
  grants read from the database.
- **oracle** `verify.py` — node2 restarted once, node1 not, and node2 kept its
  endpoint; the provider and consumer processes are the same pids and alive.
  Within 90 s the follow's events after the driver's count hold a Removed
  naming the provider's offering and, after it, an update making the same
  provider's offering available again; the follow did not end and holds the
  offering. The provider's own record shows another registration and another
  acknowledged advertise binding, and an ask to evaluate the offering for the
  consumer under the new attach. Then, apart from the follow, a fresh app on
  node1 runs a one-shot swarm discovery (`driver discover`) that finds the
  same provider alone with a complete outcome, and a fresh caller on node1
  calls the provider by its identity, retrying for 90 s, and is answered
  `echo, <that caller>`.
- **recorded, not judged** — how long the provider is gone (the gap between
  the Removed and the second update), the registrar's error at the disconnect
  and the first call error are printed in the oracle's log: the gap depends on
  node2's boot, the registrar's backoff and node1's retry of the lost member.

The follow is a long-lived app because a driver's own processes end with it,
and the restart comes after the driver: a follow opened in the driver could
not be read by the oracle. node1 is not restarted, so its follow is the one
stream that sees the whole story.

The removal reaches the follow by either of two paths, and the oracle accepts
whichever comes first: node2's coordinator retracts the provider's binding when
node2 shuts down (the runner stops a node gracefully) and sends a Removed over
the open link, or node1's `carry` (`mod/services/src/swarm.go`) calls `Lost` on
the member when the stream to node2 ends. The return is node1's `carry`
asking node2 again on its next link or after a backoff, and node2's
`Register` re-evaluating the new binding for the follows it already holds.

The oracle runs callers as programs (`driver call`, `driver discover`):
astral-py speaks the pre-v1 Services wire.

Distinct from `services-provider-gone`, which ends the provider's binding on a
running node, and from `app-node-restart-reconnect`, which proves a caller
reaches the reconnected provider but never discovers it.

Following the visibility rule of provider visibility for discovery: the
service is advertised open and the consumer holds a discovery grant from
node1's admin, so what the follow sees does not depend on the swarm-app rule. A
caller without a permit would see nothing and read `services.not_granted`; this
test does not assert that (`app-discover-local`, `request-grant-scope` and
`services-swarm` do).
