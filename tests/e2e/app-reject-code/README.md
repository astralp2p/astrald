# app-reject-code

A provider app on node2 rejects a query with code 7. An app on node2 and an
app on node1 both query it by its app identity and both read code 7: the code
reaches a caller on the same node and a caller across the link unchanged, and
reads as a rejection, not as `route_not_found`. The same provider also rejects
a second op with 255, the link-private "no route here" value; both callers read
it as the generic refusal, code 1, never as a missing route.

- **env** node · **start** `two-nodes` · **saves** —
- **driver** `script.py` — runs `driver/` (Go, this repository's astral-go):
  - node2 registers the provider with the explicit `serve_apps` permit, and the
    provider serves two ops: `reject_code_seven` rejects every query with code
    7, and `reject_code_reserved` with 255 (`IncomingQuery.RejectWithCode`);
  - `apphost.hosts` on node1 names node2 as the provider's host;
  - node2 registers a caller app, and node1 registers another; each queries
    both ops with the provider's app identity as target, once each, and the
    driver records whether each query ended in an `astral.ErrRejected`, its
    code, and the error text.
- **oracle** `verify.py` — judges the recorded facts: both callers ended in a
  rejection for each op, with code 7 for `reject_code_seven` and code 1 for
  `reject_code_reserved`. It repeats nothing.

Code 7 is also the code a node answers when it cannot reach the next hop; here
the link and the host are up, so a caller that reads 7 read the provider's
choice. A provider's 255 becomes 1 in `core.Router` (`clampReservedCode`) and,
across the link, in `linkCode` (`mod/nodes/src/mux.go`).

The code travels the provider's IPC session (`QueryRejectedMsg`,
`mod/apphost/src/ipc_handler.go`), `apphost`'s query router
(`mod/apphost/src/query_router.go`), and for the caller on node1 also the
link's response frame (`mod/nodes/src/mux.go`, `handleInboundQuery`) and the
relaying router on node1 (`query_router.go`).

node1 routes a query for the provider through the provider's relay contract,
which `apphost.register` pushes to the linked siblings in a background
goroutine (`mod/apphost/src/op_register.go`). The driver waits, within 30 s,
until `apphost.hosts` on node1 names node2 for the provider, then calls once,
with no retry, as `app-call-remote` does.

The provider's handler ends with the driver, as `apphost.bind` removes it when
its session closes. The apps' identities and tokens stay on node1 and node2.
