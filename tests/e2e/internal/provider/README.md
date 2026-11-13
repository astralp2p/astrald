# tests/e2e/internal/provider

The shared Go helper of the object-provider drivers:
`objects-provider-registration-authz`, `objects-provider-read-robustness` and
`objects-provider-revoke-lease`. It is not a test: it has no `test.toml`, so
the planner never loads it.

| file | holds |
|------|-------|
| `session.go` | `Load` of the session view, `Node` (with its `HostIP`), `UserToken`, `Connect` |
| `app.go` | `Register` of an app on a `Node` with permits, `ServeObjectsPermit`, `StoreObjectsPermit`, `RegisterAs`/`RegisterRole` by role, `Grant`, `Revoke` |
| `provider.go` | `Provider`: an app that serves the objects ops with counting handlers, and `Serve` through `apps.ServeWith` with the app's own client, on a handler bound at the node's `host_ip` |
| `ops.go` | the `objects.find`, `objects.describe`, `objects.search` and `objects.read` handlers |
| `calls.go` | `FindAll`, `DescribeAll`, `SearchAll`, `ReadAll` as a caller, and `Emit` of the facts line |

A `Provider` counts every query of each op by key: the object id for find,
describe and read, the query text for search. Each op has a mode:

- `Answer` replies with the configured answer: no identity for find, the
  descriptors set for the id for describe, the results set for search, the
  bytes set for the id for read, or a reject for an id without bytes.
- `Claim` is find naming every identity of the claim list, repeats included.
- `Hang` accepts the query and holds it open, unanswered, until the caller
  closes it, the provider stops, or 2 minutes pass. `Holds` lists how long
  each hold lasted before its caller closed it.

A `Provider` binds its handler at `tcp:<host_ip>:0` and registers the bound
address, its port resolved (astral-go `Handler.Endpoint`). The node dials that
endpoint for every query, so it reaches the test host from a Docker container
as from a local process. `Serve` fails for a node without a `host_ip`, and a
test that serves a `Provider` declares `host_listener`.

Several providers serve in one process. Each has its own client and its own
`routing.App`, and none uses the process-default client, so a driver calls
`RegisterFinder`, `RegisterDescriber` or `RegisterSearcher` itself, from a
registration hook or directly.

A driver imports it as
`github.com/astralp2p/astrald/tests/e2e/internal/provider`.
