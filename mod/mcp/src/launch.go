package mcp

import "github.com/astralp2p/astral-go/astral"

// launch wraps an agent's query for routing and marks where it came from.
//
// why the key is set: an agent is a bearer-token client over streamable HTTP, so
// its query is neither local nor an arrival over a link. Left unset the origin
// reads as local, and mod/shell serves every module's ops to a local caller. The
// refusal lives in another module and reads only this key, so every query this
// module routes goes through here — see TestOnlyLaunchRoutes.
func launch(q *astral.Query) *astral.InFlightQuery {
	inFlight := astral.Launch(q)
	inFlight.Extra.Set("origin", astral.OriginMCP)
	return inFlight
}
