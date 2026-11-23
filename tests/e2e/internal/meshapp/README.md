# tests/e2e/internal/meshapp

The long-lived mesh app of the swarm-comms tests. It is not a test: it has no
`test.toml`, so the planner never loads it. `tests/lib/meshapps.py` builds it
with `apps.build_go` and starts one process per app with `apps.spawn`.

| file | holds |
|------|-------|
| `main.go` | the `serve` and `call` commands, their flags and environment |
| `serve.go` | app registration with a discovery grant, the `hello` handler, the advertise hook |
| `follow.go` | the `reach=swarm` follow and its events |
| `calls.go` | the call loop per peer, one call, the outcome classes |
| `state.go` | the state record, the state file writer, the call log |

## meshapp serve

    meshapp serve -service S [-period 1s] [-keep-removed] [-targets id,...]
                  [-probe-nodes id,...] [-probe-op apphost.whoami] [-call-timeout 65s]

* The environment names the node: `APP_NODE_ENDPOINT`, `APP_NODE_TOKEN` (the
  node's admin token), `APP_HOST_IP` (the node's `host_ip`) and
  `ASTRAL_TESTS_APP_STATE` (the state file).
* The app registers a fresh identity on its node with `ServeApps` and a
  discovery grant for `S`.
* The app binds its handler at `tcp:<APP_HOST_IP>:0` through astral-go's
  `AppRegistrar`. The registrar re-registers the handler after a node restart.
* The `hello` op answers `hello, <caller>` and counts each caller.
* A registration hook advertises `S` open after every attach, the first and
  every reconnect.
* The app follows `S` with `reach=swarm` through astral-go's
  `FollowReconnecting`, which reopens the follow with backoff (1 s doubling to
  30 s).
* Each provider the follow reports, other than the app itself, gets one caller.
  The caller calls `hello` at once, then once per `period` while the provider
  is listed, or while it is known with `-keep-removed`. A `period` of 0 makes
  one call. Each call has the deadline `-call-timeout`.
* A call to an app answers `ok` only when the reply is `hello, <this app>`.
* `-targets` names app identities called as if the follow reported them.
* `-probe-nodes` names node identities queried with `-probe-op`, first at
  start, then once per `period`.
* SIGTERM or SIGINT ends the app. A call the app's own stop ends is not
  recorded.

## The state file

The app rewrites `$ASTRAL_TESTS_APP_STATE` whole, by rename, at most once per
second, and once more when it stops. Times are wall-clock unix seconds of the
host clock.

| field | holds |
|-------|-------|
| `pid`, `app`, `node`, `service` | the process, the app identity, the node identity, `S` |
| `started_at` | the process start |
| `registered_at`, `advertised_at` | the first handler attach, the first acknowledged advertise; `null` before |
| `attaches`, `disconnects` | handler attaches, the first and every reconnect; the registrar's disconnect errors |
| `follow_opens`, `initial` | follow boundaries reached; the last boundary `{t, complete, incomplete}` |
| `follow_err`, `not_granted`, `fatal` | the follow's terminal error; services the follow may not see; why serving ended |
| `served` | `{caller: n}`: `hello` answers per caller |
| `providers` | `{app id: peer}`: each reported provider and each `-targets` identity |
| `probes` | `{node id: peer}`: each `-probe-nodes` identity |

A peer is `{reported_at, target, listed, removed: [{t, cause}], restored: [t],
first, calls, ok, by_outcome, last_ok, last_fail, inflight_since, max_ms}`.
`first`, `last_ok` and `last_fail` are calls `{start, end, outcome, code, ms,
err, reply}`. A removal cause is the node's removal cause (`member_lost`,
`member_left`, `member_removed`, `binding_closed`, `hidden`), `none` for an
offering a reopened follow no longer shows, or `unavailable` for a provider's
own unavailable update.

## The call log

`$ASTRAL_TESTS_APP_STATE.calls.jsonl` gets one line per finished call:
`{t0, t1, to, kind, outcome, code, ms}`, where `kind` is `app` or `probe`. The
app empties the log when it starts.

## Outcome classes

| outcome | cause |
|---------|-------|
| `ok` | the target accepted and answered as expected |
| `route_not_found` | no node on the path routes the query |
| `unavailable` | rejected with code 7 |
| `timeout` | rejected with code 6, the node's timeout, or the call's deadline |
| `canceled` | the node or the caller canceled |
| `target_not_allowed` | a policy of the caller's node excludes the target |
| `rejected:<code>` | rejected with any other code |
| `error` | anything else, a wrong reply among them; `err` holds the text |

The classes are the routing outcomes of astral-docs
`core-definitions/query.md`, Routing outcomes.

## meshapp call

    meshapp call -target ID [-op hello] [-as-app] [-timeout 15s]

* The call queries `ID`'s op once, with `APP_NODE_TOKEN`, or as a fresh app
  registered on the node with `-as-app`.
* The call prints one JSON line `{caller, target, outcome, code, ms, reply,
  err}` and exits 0 whatever the outcome.
* `go test ./tests/e2e/internal/meshapp` tests the state writer, the follow
  events and the outcome classes.
