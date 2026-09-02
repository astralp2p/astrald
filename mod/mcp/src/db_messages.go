package mcp

import (
	"errors"
	"time"

	"github.com/astralp2p/astral-go/api/mcp"
	"github.com/astralp2p/astral-go/astral"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// InsertOutbox records a delivery about to be attempted and stamps the attempt.
//
// why the row is written before the delivery: a process that dies mid-delivery
// then leaves a row carrying created_at alone, which says the fate is unknown.
// Written afterwards it would leave nothing, which says the send never happened.
func (db *DB) InsertOutbox(row *dbMessage) error {
	row.Box = boxOutbox
	row.CreatedAt = time.Now().UTC()
	return db.Create(row).Error
}

// InsertInbox stores a delivered message and reports whether this call is the
// one that stored it.
//
// why the count and not the error: a collision and an honest retry both answer
// rows=0 with err=nil, and only the caller holds the sender the route
// authenticated — see storeMessage.
func (db *DB) InsertInbox(row *dbMessage) (int64, error) {
	row.Box = boxInbox
	row.CreatedAt = time.Now().UTC()
	tx := db.Clauses(clause.OnConflict{DoNothing: true}).Create(row)
	return tx.RowsAffected, tx.Error
}

// ListMessages returns one of the owner's lists, narrowed by the query and
// ordered as that list reads. It is unbounded: these rows carry no bodies, and
// the bound is on read_messages, where they are.
func (db *DB) ListMessages(owner *astral.Identity, q messageQuery) (list []dbMessage, _ error) {
	return list, q.apply(db.DB, owner).
		Order(q.order()).
		Find(&list).Error
}

// ReadMany returns the owner's messages named by the refs and stamps the inbox
// ones read. An id the owner does not hold is reported rather than refused, and
// the stamp records the first read only.
func (db *DB) ReadMany(owner *astral.Identity, refs []messageRef) (rows []dbMessage, missing []messageRef, _ error) {
	for _, ref := range refs {
		var row dbMessage
		err := db.Where("owner = ? AND box = ? AND id = ?", owner, ref.Box, ref.ID).
			Take(&row).Error
		// why the two are told apart: anything but not-found is a store that
		// could not answer, and reporting it as "you do not hold this" is a
		// falsehood about the agent's own mailbox.
		if errors.Is(err, gorm.ErrRecordNotFound) {
			missing = append(missing, ref)
			continue
		}
		if err != nil {
			return nil, nil, err
		}

		if err = db.MarkRead(owner, &row); err != nil {
			return nil, nil, err
		}

		rows = append(rows, row)
	}

	return rows, missing, nil
}

// MarkRead stamps an inbox row read, once. It is the one statement that does,
// so a body handed out anywhere says the same thing about the row it came from.
func (db *DB) MarkRead(owner *astral.Identity, row *dbMessage) error {
	if row.Box != boxInbox || row.ReadAt != nil {
		return nil
	}
	now := time.Now().UTC()
	err := db.Model(&dbMessage{}).
		Where("owner = ? AND box = ? AND id = ? AND read_at IS NULL", owner, boxInbox, row.ID).
		Update("read_at", now).Error
	if err != nil {
		return err
	}
	row.ReadAt = &now
	return nil
}

// childrenOf is the predicate every question about a message's replies narrows
// by, shared so that Children and ChildIDs answer about the same set. Archived
// rows are excluded, as they are from every listing and from the park.
//
// why a self-send collapses to its inbox row: an agent that writes to itself
// owns both rows, so a reply matches twice under one id. The inbox copy carries
// the read stamp, and it is kept only where it exists.
func childrenOf(db *gorm.DB, owner *astral.Identity, parent mcp.MessageID) *gorm.DB {
	return db.Where("owner = ? AND parent_id = ? AND archived_at IS NULL", owner, parent).
		Where(`NOT (box = 'outbox' AND EXISTS (
			SELECT 1 FROM mcp__messages self
			WHERE self.owner = mcp__messages.owner
			  AND self.box = 'inbox'
			  AND self.id = mcp__messages.id))`)
}

// Children returns the owner's messages that name this one as their parent, in
// either box, oldest first. One level: walking further is the reader's.
func (db *DB) Children(owner *astral.Identity, parent mcp.MessageID, limit int) (list []dbMessage, _ error) {
	return list, childrenOf(db.DB, owner, parent).
		Order("created_at").
		Limit(limit).
		Find(&list).Error
}

// ChildIDs answers the ids of a message's direct replies, oldest first.
//
// why the whole set and not a page: an id is what read_messages takes, so a
// reader holding all of them can ask for any reply. A count alone names none.
func (db *DB) ChildIDs(owner *astral.Identity, parent mcp.MessageID) (ids []mcp.MessageID, _ error) {
	return ids, childrenOf(db.Model(&dbMessage{}), owner, parent).
		Order("created_at").
		Pluck("id", &ids).Error
}

// SenderOf answers who wrote the row the owner holds under this id, so a caller
// can tell a delivery that collided from one that is being retried.
func (db *DB) SenderOf(owner *astral.Identity, box string, id mcp.MessageID) (*astral.Identity, error) {
	var row dbMessage
	err := db.Select("sender").
		Where("owner = ? AND box = ? AND id = ?", owner, box, id).
		Take(&row).Error
	return row.Sender, err
}

// Holds answers whether the owner has any row under this id — the question a
// parent reference asks.
//
// why either box and any archive state: a reply may answer something the owner
// sent or something it received, and archiving a message does not unsee it.
func (db *DB) Holds(owner *astral.Identity, id mcp.MessageID) (bool, error) {
	err := db.Select("seq").
		Where("owner = ? AND id = ?", owner, id).
		Take(&dbMessage{}).Error
	switch {
	case err == nil:
		return true, nil
	case errors.Is(err, gorm.ErrRecordNotFound):
		return false, nil
	default:
		return false, err
	}
}
