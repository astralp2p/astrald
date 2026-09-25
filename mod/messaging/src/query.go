package messaging

import (
	"errors"
	"fmt"
	"math"

	"github.com/astralp2p/astral-go/api/messaging"
	"github.com/astralp2p/astral-go/astral"
	"gorm.io/gorm"
)

// messageQuery is what a participant asks of its own mail. The owner is never a
// field: it is the authenticated participant, passed separately, so no value
// here can widen what the query reaches.
//
// The three lists are messaging.ListInbox, ListOutbox and ListArchive, over two
// axes. inbox and outbox are directions and live in the box column; archive is
// a state and lives in archived_at. A message is in one direction for its whole
// life and moves into and out of the archive.
type messageQuery struct {
	// List names which of the three the participant is reading. It chooses a
	// predicate rather than narrowing one.
	List string

	// From narrows to one correspondent. On an inbox row that is the sender;
	// on an outbox row the sender is the owner, so `to` is the field that
	// narrows there and `from` answers nothing.
	From *astral.Identity
	To   *astral.Identity

	// UnreadOnly and AwaitingPickup each name a column that is null by
	// construction in the other box, so each is legal in one list only and
	// refused in the others rather than silently answering everything or
	// nothing.
	UnreadOnly     bool
	AwaitingPickup bool

	// Since narrows to rows written after the one the caller last saw. It is
	// the caller's to hold: the node keeps no read position, so a lost value
	// costs a repeat and never a message.
	//
	// why the inbox only: a cursor names a position in an order, and the other
	// two lists are histories read newest-first, so a cursor on a column their
	// sort does not use can only lose rows.
	Since int64
}

var errBadNarrowing = errors.New("that filter does not apply to this list")

// validate refuses a narrowing that cannot mean anything, rather than letting
// it return everything or nothing.
func (q *messageQuery) validate() error {
	switch q.List {
	case "", messaging.ListInbox:
		q.List = messaging.ListInbox
		if q.AwaitingPickup {
			return fmt.Errorf("%w: awaiting_pickup asks about what you sent", errBadNarrowing)
		}
		if q.To != nil {
			return fmt.Errorf("%w: an inbox is narrowed by from, not to", errBadNarrowing)
		}
	case messaging.ListOutbox:
		if q.Since != 0 {
			return fmt.Errorf("%w: since pages the inbox; the outbox is a history, read newest first", errBadNarrowing)
		}
		if q.UnreadOnly {
			return fmt.Errorf("%w: unread_only asks about what you received", errBadNarrowing)
		}
		if q.From != nil {
			return fmt.Errorf("%w: you are the sender of everything here; narrow by to", errBadNarrowing)
		}
	case messaging.ListArchive:
		if q.Since != 0 {
			return fmt.Errorf("%w: since pages the inbox; the archive is a history, read newest first", errBadNarrowing)
		}
		if q.UnreadOnly || q.AwaitingPickup {
			return fmt.Errorf("%w: the archive spans both directions", errBadNarrowing)
		}
		if q.From != nil || q.To != nil {
			return fmt.Errorf("%w: the archive spans both directions, so neither from nor to picks one", errBadNarrowing)
		}
	default:
		return fmt.Errorf("no such list: %v", q.List)
	}

	return nil
}

// apply narrows a statement to what the query names. The owner is always in the
// clause and no field can widen it.
func (q messageQuery) apply(db *gorm.DB, owner *astral.Identity) *gorm.DB {
	tx := db.Where("owner = ?", owner)

	switch q.List {
	case messaging.ListArchive:
		tx = tx.Where("archived_at IS NOT NULL")
	case messaging.ListOutbox:
		tx = tx.Where("box = ? AND archived_at IS NULL", messaging.BoxOutbox)
	default:
		tx = tx.Where("box = ? AND archived_at IS NULL", messaging.BoxInbox)
	}

	if q.From != nil {
		tx = tx.Where("sender = ?", q.From)
	}
	if q.To != nil {
		tx = tx.Where("recipient = ?", q.To)
	}
	if q.UnreadOnly {
		tx = tx.Where("read_at IS NULL")
	}
	if q.AwaitingPickup {
		tx = tx.Where("landed_at IS NOT NULL AND fetched_at IS NULL")
	}
	if q.Since != 0 {
		tx = tx.Where("seq > ?", q.Since)
	}

	return tx
}

// order is the list's own and not the caller's to choose. An inbox is a queue
// worked from its head, in the order the database wrote the rows and a cursor
// pages; a sent list and an archive are histories read from their end.
//
// why the archive orders on created_at and not archived_at: the partial index
// carries created_at, and an ORDER BY on any other column sends the whole
// archive through a temp b-tree.
func (q messageQuery) order() string {
	switch q.List {
	case messaging.ListOutbox:
		return "seq desc"
	case messaging.ListArchive:
		return "created_at desc"
	default:
		return "seq"
	}
}

// messageRef names one row. The box is not optional and never inferred: an id
// alone names a row in each direction, and the archive spans both.
type messageRef struct {
	Box string
	ID  messaging.MessageID
}

// validate refuses a box that is neither direction. A row under any other box
// cannot exist, so the name is a mistake rather than a miss.
func (ref messageRef) validate() error {
	if ref.Box != messaging.BoxInbox && ref.Box != messaging.BoxOutbox {
		return fmt.Errorf("box is inbox or outbox, not %v", ref.Box)
	}
	return nil
}

// sinceOf reads a cursor a previous answer handed out. It is opaque: only its
// order means anything, and a caller that invents one past what the store can
// hold gets a refusal rather than a silently wrong page.
func sinceOf(v uint64) (int64, error) {
	if v > math.MaxInt64 {
		return 0, fmt.Errorf("since is a cursor a previous answer gave you, not %v", v)
	}
	return int64(v), nil
}
