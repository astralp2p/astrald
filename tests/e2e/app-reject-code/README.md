# app-reject-code

A provider app on node2 rejects a query with code 7. An app on node2 and an
app on node1 both query it by its app identity and both read code 7: the code
reaches a caller on the same node and a caller across the link unchanged, and
reads as a rejection, not as `route_not_found`.

- **env** node · **start** `two-nodes` · **saves** —
- **driver** `script.py` — runs `driver/` (Go, this repository's astral-go):
  - node2 registers the provider with the explicit `serve_apps` permit, and the
    provider serves one op, `reject_code_seven`, that rejects every query with
    code 7 (`IncomingQuery.RejectWithCode`);
  - `apphost.hosts` on node1 names node2 as the provider's host;
  - node2 registers a caller app, and node1 registers another; each queries
    `reject_code_seven` with the provider's app identity as target, once, and
    the driver records whether the query ended in an `astral.ErrRejected`, its
    code, and the error text.
- **oracle** `verify.py` — judges the recorded facts: both callers ended in a
  rejection, and both codes are 7. It repeats nothing.

Code 7 is neither the default reject code (1), which a node reads as a missing
route when it relays a query (`mod/nodes/src/query_router.go`, `route`), nor a
code astrald picks on its own. A caller that reads 7 read the provider's choice.

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
