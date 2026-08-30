package mcp

import (
	"time"

	mcpapi "github.com/astralp2p/astral-go/api/mcp"
	"github.com/astralp2p/astral-go/astral"
	mcpmod "github.com/astralp2p/astrald/mod/mcp"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

type DB struct {
	*gorm.DB
}

// MigrateAgents brings the agent table to the current schema.
//
// why the old columns are dropped rather than left: the node holds no
// reachability, so a column that once carried it answers nothing and a reader
// finding one would take it for a decision something still makes.
func (db *DB) MigrateAgents() error {
	m := db.Migrator()

	if m.HasTable(&dbAgent{}) {
		for _, column := range []string{"visible", "exposed"} {
			if !m.HasColumn(&dbAgent{}, column) {
				continue
			}
			if err := m.DropColumn(&dbAgent{}, column); err != nil {
				return err
			}
		}
	}

	return db.AutoMigrate(&dbAgent{})
}

// CreateAgent inserts the agent row and stamps its creation time. The caller
// builds the row, so visibility lands in the same write as the rest of the record.
func (db *DB) CreateAgent(row *dbAgent) error {
	row.CreatedAt = time.Now()
	return db.Create(row).Error
}

func (db *DB) FindAgent(identity *astral.Identity) (row *dbAgent, err error) {
	err = db.Where("identity = ?", identity).First(&row).Error
	return
}

func (db *DB) DeleteAgent(identity *astral.Identity) error {
	tx := db.Where("identity = ?", identity).Delete(&dbAgent{})
	if tx.Error != nil {
		return tx.Error
	}
	if tx.RowsAffected == 0 {
		return gorm.ErrRecordNotFound
	}
	return nil
}

func (db *DB) ListAgents() (list []dbAgent, _ error) {
	return list, db.Find(&list).Error
}

// dbMessage is one message in a recipient's inbox. The row is the whole of the
// message's state: stored_at is stamped on arrival, read_at when the recipient
// claims or opens it, and the two receipt columns track the fact owed to a
// sender on another node.
type dbMessage struct {
	// note: the id is the sender's, minted before delivery, so a delivery that
	// arrives twice collides here and is stored once.
	//
	// fixme: the key is the id alone, so one id space spans every inbox on the
	// node. A sender re-using an id across two recipients loses the second
	// delivery. The key an inbox needs is (recipient, id).
	ID        mcpapi.MessageID `gorm:"primaryKey"`
	Sender    *astral.Identity `gorm:"index"`
	Recipient *astral.Identity `gorm:"index:idx_mcp_messages_inbox,priority:1"`
	Content   string

	// StoredAt is when this node wrote the row. It is a claim about the node
	// and not about the recipient, who may not run for days — beside an outbox
	// whose own success state would also be called delivered, one word would
	// name two things.
	StoredAt time.Time `gorm:"index:idx_mcp_messages_inbox,priority:2"`

	ReadAt *time.Time

	// ReceiptDueAt is set when the body is first handed out and the sender is
	// not on this node. A local sender's outbox row is stamped directly and
	// never becomes due — see noteFetched.
	//
	// why the fact is recorded even though only one attempt is made: a receipt
	// lost in transit is the sender left believing a message was never
	// collected. The row is what a sweep would read if one is ever written,
	// and it costs one column to leave that door open.
	ReceiptDueAt *time.Time

	// ReceiptStoredAt is the sender's node acknowledging our receipt.
	ReceiptStoredAt *time.Time
}

func (dbMessage) TableName() string {
	return mcpmod.DBPrefix + "messages"
}

// MigrateMessages brings the message table to the current schema.
//
// why the rename is its own step: AutoMigrate adds a column, it never renames
// one, so without this a stored table keeps delivered_at and grows an empty
// stored_at beside it — every message already held reads as never stored.
func (db *DB) MigrateMessages() error {
	m := db.Migrator()

	if m.HasTable(&dbMessage{}) &&
		m.HasColumn(&dbMessage{}, "delivered_at") &&
		!m.HasColumn(&dbMessage{}, "stored_at") {
		if err := m.RenameColumn(&dbMessage{}, "delivered_at", "stored_at"); err != nil {
			return err
		}
	}

	return db.AutoMigrate(&dbMessage{})
}

// InsertMessage stores a delivered message and stamps its arrival. A message
// whose id is already stored is left as it stands, so a sender that retries
// after a lost acknowledgement delivers once.
func (db *DB) InsertMessage(row *dbMessage) error {
	row.StoredAt = time.Now().UTC()
	return db.Clauses(clause.OnConflict{DoNothing: true}).Create(row).Error
}

// ListInbox returns the messages addressed to the recipient, oldest first.
func (db *DB) ListInbox(recipient *astral.Identity, unreadOnly bool, limit int) (list []dbMessage, _ error) {
	tx := db.Where("recipient = ?", recipient)
	if unreadOnly {
		tx = tx.Where("read_at IS NULL")
	}
	return list, tx.Order("stored_at").Limit(limit).Find(&list).Error
}

// ReadMessage returns one message addressed to the recipient and stamps it
// read. A message already read is returned as it stands: reading is not a
// claim, and the stamp records the first read.
// Returns gorm.ErrRecordNotFound when the recipient holds no such message.
func (db *DB) ReadMessage(recipient *astral.Identity, id mcpapi.MessageID) (*dbMessage, error) {
	var row dbMessage

	err := db.Where("recipient = ? AND id = ?", recipient, id).First(&row).Error
	if err != nil {
		return nil, err
	}

	if row.ReadAt == nil {
		now := time.Now().UTC()
		if err = db.Model(&dbMessage{}).Where("id = ?", id).Update("read_at", now).Error; err != nil {
			return nil, err
		}
		row.ReadAt = &now
	}

	return &row, nil
}

// ClaimNext stamps the oldest unread message addressed to the recipient and
// returns it. Returns gorm.ErrRecordNotFound when the inbox holds none.
//
// why the update names read_at again: two claims can select the same row, and
// the update is where one of them loses. A claim that changes no row starts
// over and takes the next message rather than the one it lost.
func (db *DB) ClaimNext(recipient *astral.Identity) (*dbMessage, error) {
	for {
		var row dbMessage

		err := db.
			Where("recipient = ? AND read_at IS NULL", recipient).
			Order("stored_at").
			First(&row).Error
		if err != nil {
			return nil, err
		}

		now := time.Now().UTC()

		tx := db.Model(&dbMessage{}).
			Where("id = ? AND read_at IS NULL", row.ID).
			Update("read_at", now)
		if tx.Error != nil {
			return nil, tx.Error
		}
		if tx.RowsAffected == 0 {
			continue
		}

		row.ReadAt = &now
		return &row, nil
	}
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

// StampReceiptStored records the sender's node acknowledging our receipt. It
// writes an inbox row: the fact is the recipient's, about a receipt it sent.
func (db *DB) StampReceiptStored(id mcpapi.MessageID) error {
	return db.Model(&dbMessage{}).
		Where("id = ? AND receipt_stored_at IS NULL", id).
		Update("receipt_stored_at", time.Now().UTC()).Error
}

// outboxErrLimit bounds the recipient's node's own words. The string is another
// operator's, so it is quoted and never trusted for its length.
const outboxErrLimit = 256

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

// SetOutboxErr records the recipient's node's own words for a refusal.
func (db *DB) SetOutboxErr(id mcpapi.MessageID, text string) error {
	if len(text) > outboxErrLimit {
		text = text[:outboxErrLimit]
	}
	return db.Model(&dbOutbox{}).
		Where("id = ? AND err = ?", id, "").
		Update("err", text).Error
}

// ListOutbox returns what the sender sent, narrowed by the query and newest
// first unless it asks otherwise.
//
// why newest first by default where the inbox is oldest first: an inbox is a
// queue and is worked from its head, and a sent list is a history and is read
// from its end.
//
// why the sender is always in the where clause: an agent's own sends are the
// only ones it may read, and no field of the query can widen that.
func (db *DB) ListOutbox(sender *astral.Identity, q outboxQuery) (list []dbOutbox, _ error) {
	tx := db.Where("sender = ?", sender)

	if !q.ID.IsZero() {
		tx = tx.Where("id = ?", q.ID)
	}

	// why stored_at is required and not just fetched_at absent: a delivery that
	// failed, or one whose fate was never learned, is not waiting on the
	// recipient. Only a message known to be in their mailbox is.
	if q.AwaitingPickup {
		tx = tx.Where("stored_at IS NOT NULL AND fetched_at IS NULL")
	}

	order := "sent_at desc"
	if q.OldestFirst {
		order = "sent_at"
	}

	return list, tx.Order(order).Limit(q.Limit).Find(&list).Error
}
