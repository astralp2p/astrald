package mcp

import (
	"time"
	"unicode/utf8"

	"github.com/astralp2p/astral-go/api/mcp"
	"github.com/astralp2p/astral-go/astral"
)

// errLimit bounds the recipient's node's own words: unbounded, they are a remote
// peer deciding how much of a local context window to occupy.
const errLimit = 256

// clip cuts text to n bytes on a rune boundary and marks the cut, so a reader
// is not left thinking it read the whole of it.
func clip(text string, n int) string {
	if len(text) <= n {
		return text
	}
	for n > 0 && !utf8.RuneStart(text[n]) {
		n--
	}
	return text[:n] + "… (cut)"
}

// Archive stamps one of the owner's messages put away and reports whether this
// call is the one that stamped it.
//
// why admission and write are one statement: a lookup then a write would race,
// and the answer the tool owes — did I put it away, or was it already away — is
// exactly what the one statement returns.
func (db *DB) Archive(owner *astral.Identity, box string, id mcp.MessageID) (int64, error) {
	tx := db.Model(&dbMessage{}).
		Where("owner = ? AND box = ? AND id = ? AND archived_at IS NULL", owner, box, id).
		Update("archived_at", time.Now().UTC())
	return tx.RowsAffected, tx.Error
}

// Unarchive clears the stamp. Archive is the agent's own bookkeeping and
// crosses no link, so it is the one stamp here with an inverse.
func (db *DB) Unarchive(owner *astral.Identity, box string, id mcp.MessageID) (int64, error) {
	tx := db.Model(&dbMessage{}).
		Where("owner = ? AND box = ? AND id = ? AND archived_at IS NOT NULL", owner, box, id).
		Update("archived_at", nil)
	return tx.RowsAffected, tx.Error
}

// Each stamp below writes once: a second call changes nothing, because the first
// answer is the one that happened. Each names owner and box, because an id is
// the peer's to mint and one owner may hold two rows under it.

// MarkReceiptDue records that a receipt is owed on this inbox row and reports
// whether this call is the one that recorded it.
func (db *DB) MarkReceiptDue(recipient *astral.Identity, id mcp.MessageID) (int64, error) {
	tx := db.Model(&dbMessage{}).
		Where("owner = ? AND box = ? AND id = ? AND receipt_due_at IS NULL",
			recipient, boxInbox, id).
		Update("receipt_due_at", time.Now().UTC())
	return tx.RowsAffected, tx.Error
}

// StampReceiptStored records the sender's node acknowledging our receipt.
func (db *DB) StampReceiptStored(recipient *astral.Identity, id mcp.MessageID) error {
	return db.Model(&dbMessage{}).
		Where("owner = ? AND box = ? AND id = ? AND receipt_stored_at IS NULL",
			recipient, boxInbox, id).
		Update("receipt_stored_at", time.Now().UTC()).Error
}

// StampLanded records the recipient's node acknowledging the write.
func (db *DB) StampLanded(sender *astral.Identity, id mcp.MessageID) error {
	return db.Model(&dbMessage{}).
		Where("owner = ? AND box = ? AND id = ? AND landed_at IS NULL",
			sender, boxOutbox, id).
		Update("landed_at", time.Now().UTC()).Error
}

// StampFailed records a delivery known not to have been stored.
func (db *DB) StampFailed(sender *astral.Identity, id mcp.MessageID) error {
	return db.Model(&dbMessage{}).
		Where("owner = ? AND box = ? AND id = ? AND failed_at IS NULL",
			sender, boxOutbox, id).
		Update("failed_at", time.Now().UTC()).Error
}

// StampFetched records the body handed out, for a sender on this node.
//
// why matching nothing is not an error: the sender may hold no row — a message
// from another node, or one whose row this node never wrote.
func (db *DB) StampFetched(sender *astral.Identity, id mcp.MessageID) error {
	return db.Model(&dbMessage{}).
		Where("owner = ? AND box = ? AND id = ? AND fetched_at IS NULL",
			sender, boxOutbox, id).
		Update("fetched_at", time.Now().UTC()).Error
}

// StampFetchedFrom is the receipt's admission and its write in one statement:
// the row must be ours, must be one we sent to this caller, and must not already
// be stamped. RowsAffected is the answer.
//
// why sender is spent as owner: on an outbox row the generated column makes
// them equal, and naming it twice adds an unindexed equality.
func (db *DB) StampFetchedFrom(sender, recipient *astral.Identity, id mcp.MessageID) (int64, error) {
	tx := db.Model(&dbMessage{}).
		Where("owner = ? AND box = ? AND id = ? AND recipient = ? AND fetched_at IS NULL",
			sender, boxOutbox, id, recipient).
		Update("fetched_at", time.Now().UTC())
	return tx.RowsAffected, tx.Error
}

// SetErr records the recipient's node's own words for a refusal, bounded here.
//
// why the guard is IS NULL and not an empty-string comparison: an empty string
// is a refusal whose words were empty, so comparing against it matches nothing
// and discards every refusal in silence.
func (db *DB) SetErr(sender *astral.Identity, id mcp.MessageID, text string) error {
	return db.Model(&dbMessage{}).
		Where("owner = ? AND box = ? AND id = ? AND err IS NULL", sender, boxOutbox, id).
		Update("err", clip(text, errLimit)).Error
}
