package messaging

import (
	"github.com/astralp2p/astral-go/astral"
	"github.com/astralp2p/astral-go/lib/routing"
)

// refusesOrigin answers whether the query came from where no messaging
// operation is served: over a link, or from an agent over MCP.
//
// why the MCP origin is refused here as well as in mod/shell: shell refuses it
// only where it mounts this router, and an operation that trusted its mount
// point would serve an agent wherever the router is mounted next.
func refusesOrigin(q *routing.IncomingQuery) bool {
	return q.Origin() == astral.OriginNetwork || q.Origin() == astral.OriginMCP
}

// admitsMailCaller answers whether a mail operation serves this query: an
// origin this module serves, and a caller whose mailbox this node hosts.
//
// why the caller and never an argument names the mailbox: every mail operation
// acts on the caller's own boxes, so no value a caller sends can reach another
// participant's.
func (mod *Module) admitsMailCaller(q *routing.IncomingQuery) bool {
	return !refusesOrigin(q) && mod.hosts(q.Caller())
}
