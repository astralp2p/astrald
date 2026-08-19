package mcp

import "github.com/astralp2p/astral-go/astral"

// launch wraps an agent's query for routing and marks where it came from.
//
// why: the agent is a remote client authenticated by bearer token over
// streamable HTTP (mcp_server.go), so its query is neither local nor an arrival
// over a link. Left unset, the origin key reads as local (astral-go
// InFlightQuery.IsLocal), and mod/shell serves every module's ops to a local
// caller.
//
// why a helper and not the call site: the refusal lives in another module and
// reads only this key, so a query this module routes without it reaches node
// ops. Routing through here is what keeps that from being a per-call-site
// promise — see TestOnlyLaunchRoutes.
func launch(q *astral.Query) *astral.InFlightQuery {
	inFlight := astral.Launch(q)
	inFlight.Extra.Set("origin", astral.OriginMCP)
	return inFlight
}
