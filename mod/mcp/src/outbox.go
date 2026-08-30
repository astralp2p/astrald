package mcp

import (
	mcpapi "github.com/astralp2p/astral-go/api/mcp"
	"github.com/astralp2p/astral-go/astral"
)

const (
	defaultOutboxLimit = 50
	maxOutboxLimit     = 200
)

// outboxQuery is what a sender asks of its own sent list.
//
// why a struct and not three arguments: the three narrow the same list and
// compose, and a call site naming them by field says which question is being
// asked.
type outboxQuery struct {
	// ID answers one send. The zero value asks about all of them.
	ID mcpapi.MessageID

	// AwaitingPickup narrows to sends the recipient's node stored and has not
	// handed out — the ones still waiting on the recipient, and the question a
	// sender waiting for an answer actually has. A delivery that failed waits
	// on nobody and is excluded.
	AwaitingPickup bool

	// OldestFirst reverses the order. A sent list reads newest first, because
	// it is a history; a sender chasing what is outstanding wants the other
	// end, and those are the rows a newest-first cap drops.
	OldestFirst bool

	Limit int
}

// listOutbox returns what the sender sent, narrowed by the query.
//
// why the cap is here and not in the tool: how much of a list the module hands
// out in one answer is the module's rule and not the caller's. A second caller
// — an operation serving the owner's dashboard — reads the same limit rather
// than choosing its own.
func (mod *Module) listOutbox(sender *astral.Identity, q outboxQuery) ([]dbOutbox, error) {
	if q.Limit <= 0 {
		q.Limit = defaultOutboxLimit
	}
	q.Limit = min(q.Limit, maxOutboxLimit)

	return mod.db.ListOutbox(sender, q)
}
