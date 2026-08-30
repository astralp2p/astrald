package mcp

import (
	"time"

	mcpapi "github.com/astralp2p/astral-go/api/mcp"
	"github.com/astralp2p/astral-go/astral"
	mcpmod "github.com/astralp2p/astrald/mod/mcp"
)

// dbOutbox is what a sender kept of a delivery it performed.
//
// why a second table and not a flag on dbMessage: the two rows have different
// owners. A recipient's row is the recipient's, and across nodes it is not on
// this machine at all. What a sender knows is the sender's, and it survives
// whether or not the recipient's node is reachable.
//
// why timestamps and no status column: every field records when a fact became
// true, and a null is the absence of that fact rather than a value somebody had
// to choose. A row nothing has updated is then the row nobody knows the fate
// of, which is the honest answer after a crash.
type dbOutbox struct {
	ID        mcpapi.MessageID `gorm:"primaryKey"`
	Sender    *astral.Identity `gorm:"index:idx_mcp_outbox,priority:1"`
	Recipient *astral.Identity

	// SentAt is when this node took the message. Never null: the row exists
	// because a delivery was attempted. It is the sort key and the clock an
	// escalation counts from.
	SentAt time.Time `gorm:"index:idx_mcp_outbox,priority:2"`

	// StoredAt is the recipient's node acknowledging the write.
	StoredAt *time.Time

	// FailedAt is a delivery known not to have been stored. Not set when the
	// answer merely never arrived — see errNoAnswer.
	FailedAt *time.Time

	// FetchedAt is written by the recipient's side, never by the sender's:
	// directly when both agents are on this node, by an mcp.receipt when they
	// are not.
	FetchedAt *time.Time

	// Err is the recipient's node's own words when it judged the message and
	// said no, which tells a judgement apart from a delivery that never
	// arrived. A message rejected once would be rejected the same way again.
	Err string

	// Thread names the exchange this send belongs to, mirroring the recipient's
	// row. It is what lets a sender read its own side of one conversation.
	Thread mcpapi.MessageID `gorm:"index"`

	// Content is what was sent, kept. Nothing in this change reads it: it is
	// written so that what an agent said is answerable at all, and so that a
	// resend, if one is ever added, has a history to work from.
	Content string
}

func (dbOutbox) TableName() string {
	return mcpmod.DBPrefix + "outbox"
}
