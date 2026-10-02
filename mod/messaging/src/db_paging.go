package messaging

import (
	"github.com/astralp2p/astral-go/api/messaging"
	"github.com/astralp2p/astral-go/astral"
	"gorm.io/gorm"
)

// envelopeColumns are every message column but content. A page and a change
// read hands no body out, so it reads none: the bound on a page is then a bound
// on what the store materializes, not only on what crosses the wire.
const envelopeColumns = `seq, box, id, sender, recipient, owner, peer, parent_id, created_at,
  archived_at, read_at, receipt_due_at, receipt_stored_at, landed_at, failed_at, fetched_at, err, rev`

// pageQuery is one page of a mailbox read backwards: one list, or one peer's
// unarchived rows in both boxes. Before is a seq the previous page answered,
// zero for the newest.
type pageQuery struct {
	List   string
	Peer   *astral.Identity
	Before int64
	Limit  int
}

// rowPage is what a page or a change read found, with the positions read in
// the same transaction.
//
// Next is the seq to pass back as before, or the rev to pass back as since;
// zero on a page means the page reached the end. More says a change read left
// rows behind. Rev is the allocator as the read found it, and Generation the
// mailbox's.
type rowPage struct {
	Rows       []dbMessage
	Next       int64
	More       bool
	Rev        int64
	Generation int64
}

// PageMessages reads one page newest first.
//
// why limit+1: the extra row is never answered. It says whether another page
// exists, so a page that ends exactly at the limit still answers the end.
func (db *DB) PageMessages(owner *astral.Identity, q pageQuery) (page rowPage, err error) {
	err = db.Transaction(func(tx *gorm.DB) error {
		stmt := tx.Model(&dbMessage{}).Select(envelopeColumns).Where("owner = ?", owner)
		switch {
		case q.Peer != nil:
			stmt = stmt.Where("peer = ? AND archived_at IS NULL", q.Peer)
		case q.List == messaging.ListArchive:
			stmt = stmt.Where("archived_at IS NOT NULL")
		case q.List == messaging.ListOutbox:
			stmt = stmt.Where("box = ? AND archived_at IS NULL", messaging.BoxOutbox)
		default:
			stmt = stmt.Where("box = ? AND archived_at IS NULL", messaging.BoxInbox)
		}
		if q.Before > 0 {
			stmt = stmt.Where("seq < ?", q.Before)
		}
		if err := stmt.Order("seq DESC").Limit(q.Limit + 1).Find(&page.Rows).Error; err != nil {
			return err
		}
		if len(page.Rows) > q.Limit {
			page.Rows = page.Rows[:q.Limit]
			page.Next = page.Rows[q.Limit-1].Seq
		}
		return positions(tx, owner, &page.Rev, &page.Generation)
	})
	return page, err
}

// MessageChanges reads the rows that changed after since, oldest change first,
// archived rows included: a row put away or taken back out is a change.
func (db *DB) MessageChanges(owner, peer *astral.Identity, since int64, limit int) (page rowPage, err error) {
	err = db.Transaction(func(tx *gorm.DB) error {
		stmt := tx.Model(&dbMessage{}).Select(envelopeColumns).Where("owner = ?", owner)
		if peer != nil {
			stmt = stmt.Where("peer = ?", peer)
		}
		err := stmt.Where("rev > ?", since).Order("rev").Limit(limit + 1).Find(&page.Rows).Error
		if err != nil {
			return err
		}
		if len(page.Rows) > limit {
			page.Rows = page.Rows[:limit]
			page.More = true
		}
		page.Next = since
		if n := len(page.Rows); n > 0 {
			page.Next = page.Rows[n-1].Rev
		}
		return positions(tx, owner, &page.Rev, &page.Generation)
	})
	return page, err
}

// dbConversation is one summary with its latest row, which a tombstone lacks.
type dbConversation struct {
	Peer      *astral.Identity
	LatestSeq *int64
	Unread    int64
	Rev       int64
	Latest    *dbMessage
}

// conversationPage is a page or a change read of summaries.
type conversationPage struct {
	Conversations []dbConversation
	Next          int64
	More          bool
	Rev           int64
	Generation    int64
}

// PageConversations reads the conversations newest first, by their latest
// message, and leaves tombstones out: a conversation with nothing unarchived is
// not one the list draws. With a peer it reads that one conversation, which a
// tombstone or nothing may answer.
func (db *DB) PageConversations(owner, peer *astral.Identity, before int64, limit int) (page conversationPage, err error) {
	err = db.Transaction(func(tx *gorm.DB) error {
		stmt := tx.Table(tableConversations).Select("peer, latest_seq, unread, rev").Where("owner = ?", owner)
		if peer != nil {
			stmt = stmt.Where("peer = ?", peer)
		} else {
			stmt = stmt.Where("latest_seq IS NOT NULL")
			if before > 0 {
				stmt = stmt.Where("latest_seq < ?", before)
			}
			stmt = stmt.Order("latest_seq DESC").Limit(limit + 1)
		}
		if err := stmt.Scan(&page.Conversations).Error; err != nil {
			return err
		}
		if peer == nil && len(page.Conversations) > limit {
			page.Conversations = page.Conversations[:limit]
			page.Next = *page.Conversations[limit-1].LatestSeq
		}
		if err := withLatest(tx, page.Conversations); err != nil {
			return err
		}
		return positions(tx, owner, &page.Rev, &page.Generation)
	})
	return page, err
}

// ConversationChanges reads the summaries that changed after since, oldest
// change first, tombstones included: a conversation whose last message was put
// away is a change a list must see to drop the row.
func (db *DB) ConversationChanges(owner *astral.Identity, since int64, limit int) (page conversationPage, err error) {
	err = db.Transaction(func(tx *gorm.DB) error {
		err := tx.Table(tableConversations).Select("peer, latest_seq, unread, rev").
			Where("owner = ? AND rev > ?", owner, since).
			Order("rev").Limit(limit + 1).Scan(&page.Conversations).Error
		if err != nil {
			return err
		}
		if len(page.Conversations) > limit {
			page.Conversations = page.Conversations[:limit]
			page.More = true
		}
		page.Next = since
		if n := len(page.Conversations); n > 0 {
			page.Next = page.Conversations[n-1].Rev
		}
		if err := withLatest(tx, page.Conversations); err != nil {
			return err
		}
		return positions(tx, owner, &page.Rev, &page.Generation)
	})
	return page, err
}

// withLatest reads each conversation's latest row, envelope columns only.
//
// why one seek per summary and not a join: seq is the primary key, so each
// read is one lookup, and a page holds at most the ceiling of them.
func withLatest(tx *gorm.DB, list []dbConversation) error {
	for i := range list {
		if list[i].LatestSeq == nil {
			continue
		}
		var row dbMessage
		err := tx.Model(&dbMessage{}).Select(envelopeColumns).
			Where("seq = ?", *list[i].LatestSeq).Take(&row).Error
		if err != nil {
			return err
		}
		list[i].Latest = &row
	}
	return nil
}

// positions reads the allocator and the mailbox's generation in the caller's
// transaction, so they describe the same state as the rows beside them.
func positions(tx *gorm.DB, owner *astral.Identity, rev, generation *int64) error {
	if err := tx.Raw(`SELECT rev FROM messaging__revisions WHERE id = 1`).Scan(rev).Error; err != nil {
		return err
	}
	return tx.Raw(`SELECT COALESCE((SELECT generation FROM messaging__generations WHERE owner = ?), 0)`, owner).
		Scan(generation).Error
}
