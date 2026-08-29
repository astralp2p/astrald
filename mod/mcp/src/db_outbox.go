package mcp

import (
	"time"

	mcpapi "github.com/astralp2p/astral-go/api/mcp"
	"github.com/astralp2p/astral-go/astral"
	mcpmod "github.com/astralp2p/astrald/mod/mcp"
)

// outboxErrLimit bounds the recipient's node's own words. The string is another
// operator's, so it is quoted and never trusted for its length.
const outboxErrLimit = 256

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

	// Content is what was sent, kept. Nothing in this change reads it: it is
	// written so that what an agent said is answerable at all, and so that a
	// resend, if one is ever added, has a history to work from.
	Content string
}

func (dbOutbox) TableName() string {
	return mcpmod.DBPrefix + "outbox"
}

// MigrateOutbox brings the outbox table to the current schema.
func (db *DB) MigrateOutbox() error {
	return db.AutoMigrate(&dbOutbox{})
}

// InsertOutbox records a delivery about to be attempted and stamps the attempt.
//
// why the row is written before the delivery and not after: a process that dies
// mid-delivery leaves a row carrying sent_at alone, which says the fate is
// unknown. Written afterwards it would leave nothing, which says the send never
// happened.
func (db *DB) InsertOutbox(row *dbOutbox) error {
	row.SentAt = time.Now().UTC()
	return db.Create(row).Error
}

// Each stamp is its own method and each writes once: a second call changes
// nothing, because the first answer is the one that happened.
//
// why a method per column and not one taking a column name: a column passed in
// is a query built by concatenation, and the call sites are fixed. Naming them
// also puts the meaning where a reader is.

// StampOutboxStored records the recipient's node acknowledging the write.
func (db *DB) StampOutboxStored(id mcpapi.MessageID) error {
	return db.Model(&dbOutbox{}).
		Where("id = ? AND stored_at IS NULL", id).
		Update("stored_at", time.Now().UTC()).Error
}

// StampOutboxFailed records a delivery known not to have been stored.
func (db *DB) StampOutboxFailed(id mcpapi.MessageID) error {
	return db.Model(&dbOutbox{}).
		Where("id = ? AND failed_at IS NULL", id).
		Update("failed_at", time.Now().UTC()).Error
}

// StampOutboxFetched records the body handed out, for a sender on this node.
//
// why matching nothing is not an error: the sender may hold no row — a message
// from before this table existed, or one from another node.
func (db *DB) StampOutboxFetched(id mcpapi.MessageID) error {
	return db.Model(&dbOutbox{}).
		Where("id = ? AND fetched_at IS NULL", id).
		Update("fetched_at", time.Now().UTC()).Error
}

// StampReceiptStored records the sender's node acknowledging our receipt. It
// writes an inbox row: the fact is the recipient's, about a receipt it sent.
func (db *DB) StampReceiptStored(id mcpapi.MessageID) error {
	return db.Model(&dbMessage{}).
		Where("id = ? AND receipt_stored_at IS NULL", id).
		Update("receipt_stored_at", time.Now().UTC()).Error
}

// StampOutboxFetchedFrom is the receipt's admission and its write in one
// statement: the row must be ours, must be one we sent to this caller, and must
// not already be stamped. RowsAffected is the answer.
func (db *DB) StampOutboxFetchedFrom(sender, recipient *astral.Identity, id mcpapi.MessageID) (int64, error) {
	tx := db.Model(&dbOutbox{}).
		Where("id = ? AND sender = ? AND recipient = ? AND fetched_at IS NULL",
			id, sender, recipient).
		Update("fetched_at", time.Now().UTC())
	return tx.RowsAffected, tx.Error
}

// MarkReceiptDue records that a receipt is owed on this inbox row and reports
// whether this call is the one that recorded it.
//
// why the caller needs the count: one attempt is made, and the attempt belongs
// to whichever read first handed the body out. A later read finds the row
// already due and sends nothing.
func (db *DB) MarkReceiptDue(id mcpapi.MessageID) (int64, error) {
	tx := db.Model(&dbMessage{}).
		Where("id = ? AND receipt_due_at IS NULL", id).
		Update("receipt_due_at", time.Now().UTC())
	return tx.RowsAffected, tx.Error
}

// SetOutboxErr records the recipient's node's own words for a refusal.
func (db *DB) SetOutboxErr(id mcpapi.MessageID, text string) error {
	if len(text) > outboxErrLimit {
		text = text[:outboxErrLimit]
	}
	return db.Model(&dbOutbox{}).
		Where("id = ? AND err = ?", id, "").
		Update("err", text).Error
}

// ListOutbox returns what the sender sent, newest first.
//
// why newest first where the inbox is oldest first: an inbox is a queue and is
// worked from its head, and a sent list is a history and is read from its end.
func (db *DB) ListOutbox(sender *astral.Identity, limit int) (list []dbOutbox, _ error) {
	return list, db.
		Where("sender = ?", sender).
		Order("sent_at desc").
		Limit(limit).
		Find(&list).Error
}
