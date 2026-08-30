package mcp

import (
	mcpapi "github.com/astralp2p/astral-go/api/mcp"
	"github.com/astralp2p/astral-go/astral"
	"gorm.io/gorm"
)

const (
	defaultInboxLimit = 50
	maxInboxLimit     = 200
)

// inboxQuery is what a recipient asks of its own inbox.
//
// why a reader names what it wants: a claim is permanent and there is no way
// to give a message back, so a reader waiting on one answer must not be able
// to take a message it did not ask for. Erlang's selective receive is the same
// idea and this is the same one WHERE clause.
type inboxQuery struct {
	// From narrows to one sender. Nil asks about all of them.
	From *astral.Identity

	// Thread narrows to one exchange. The zero value asks about all of them.
	Thread mcpapi.MessageID

	// UnreadOnly drops what has already been handed out. A claim sets it
	// whatever the caller asked, because a claim is only ever of an unread
	// message.
	UnreadOnly bool

	Limit int
}

// apply narrows a statement to what the query names. The recipient is always
// in the clause and no field can widen it.
func (q inboxQuery) apply(db *gorm.DB, recipient *astral.Identity) *gorm.DB {
	tx := db.Where("recipient = ?", recipient)

	if q.From != nil {
		tx = tx.Where("sender = ?", q.From)
	}
	if !q.Thread.IsZero() {
		tx = tx.Where("thread = ?", q.Thread)
	}
	if q.UnreadOnly {
		tx = tx.Where("read_at IS NULL")
	}

	return tx
}

// listInbox returns the messages waiting for the recipient, oldest first.
//
// why the cap is here and not in the tool: how much of an inbox the module
// hands out in one answer is the module's rule and not the caller's. A second
// caller — an operation serving the owner's dashboard — reads the same limit
// rather than choosing its own.
func (mod *Module) listInbox(recipient *astral.Identity, q inboxQuery) ([]dbMessage, error) {
	if q.Limit <= 0 {
		q.Limit = defaultInboxLimit
	}
	q.Limit = min(q.Limit, maxInboxLimit)

	return mod.db.ListInbox(recipient, q)
}
