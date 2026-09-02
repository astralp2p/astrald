package mcp

import (
	"errors"
	"fmt"
	"strconv"

	"github.com/astralp2p/astral-go/api/mcp"
	"github.com/astralp2p/astral-go/astral"
	"gorm.io/gorm"
)

// The three names an agent reads, and the two axes underneath them. inbox and
// outbox are directions and live in the box column; archive is a state and
// lives in archived_at. A message is in one direction for its whole life and
// moves into and out of the archive.
const (
	listInbox   = "inbox"
	listOutbox  = "outbox"
	listArchive = "archive"
)

const (
	boxInbox  = "inbox"
	boxOutbox = "outbox"
)

// messageQuery is what an agent asks of its own mail. The owner is never a
// field: it is the authenticated agent, passed separately, so no value here can
// widen what the query reaches.
type messageQuery struct {
	// List names which of the three the agent is reading. It chooses a
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

	// Since narrows to rows the database wrote after the one the caller last
	// saw. It is the caller's to hold: the node keeps no read position, so a
	// lost or stale value costs a repeat and never a message.
	//
	// why a sequence and not an instant: created_at is read in Go before the
	// INSERT, so a row can carry an earlier instant and commit later, and a
	// cursor over it steps past a message that had not appeared when the cursor
	// was handed out. seq is assigned under the write lock, so paging it can
	// only ever narrow.
	//
	// why the inbox only: a cursor names a position in an order, and the other
	// two lists are histories read newest-first. A cursor on a column the sort
	// does not use can only lose rows, so the other two refuse it rather than
	// answer it wrongly.
	Since int64
}

var errBadNarrowing = errors.New("that filter does not apply to this list")

// validate refuses a narrowing that cannot mean anything, rather than letting
// it return everything or nothing.
func (q *messageQuery) validate() error {
	switch q.List {
	case "", listInbox:
		q.List = listInbox
		if q.AwaitingPickup {
			return fmt.Errorf("%w: awaiting_pickup asks about what you sent", errBadNarrowing)
		}
		if q.To != nil {
			return fmt.Errorf("%w: an inbox is narrowed by from, not to", errBadNarrowing)
		}
	case listOutbox:
		if q.Since != 0 {
			return fmt.Errorf("%w: since pages the inbox; the outbox is a history, read newest first", errBadNarrowing)
		}
		if q.UnreadOnly {
			return fmt.Errorf("%w: unread_only asks about what you received", errBadNarrowing)
		}
		if q.From != nil {
			return fmt.Errorf("%w: you are the sender of everything here; narrow by to", errBadNarrowing)
		}
	case listArchive:
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
	case listArchive:
		tx = tx.Where("archived_at IS NOT NULL")
	case listOutbox:
		tx = tx.Where("box = ? AND archived_at IS NULL", boxOutbox)
	default:
		tx = tx.Where("box = ? AND archived_at IS NULL", boxInbox)
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

// order is the list's own, and it is not the caller's to choose.
//
// An inbox is a queue and is worked from its head, so it reads in the order the
// database wrote the rows — which is the order a cursor pages. A sent list and
// an archive are histories and are read from their end.
//
// why the archive orders on created_at and not archived_at: the partial index
// carries created_at, and an ORDER BY on any other column sends the whole
// archive through a temp b-tree — measured four times the cost of the other two
// listings, degrading to a full scan once the table is mostly archived.
func (q messageQuery) order() string {
	switch q.List {
	case listOutbox:
		return "seq desc"
	case listArchive:
		return "created_at desc"
	default:
		return "seq"
	}
}

// messageRef names one row. The box is not optional and never inferred: an id
// alone names a row in each direction, and the archive spans both.
type messageRef struct {
	Box string
	ID  mcp.MessageID
}

// nextSince is the furthest the answer reached in the database's own order, so
// a caller passing it back sees only what was written after. It is the caller's
// to hold: the node keeps no read position, so a lost value costs a repeat
// rather than a message.
//
// why the sequence and not a timestamp: created_at is read before the row is
// written, so a message can carry an earlier instant and appear later. A cursor
// over it steps past mail that had not arrived yet, permanently.
func nextSince(rows []dbMessage) string {
	var furthest int64
	for _, row := range rows {
		if row.Seq > furthest {
			furthest = row.Seq
		}
	}
	if furthest == 0 {
		return ""
	}
	return strconv.FormatInt(furthest, 10)
}

// parseSince reads a cursor a previous answer handed out. It is opaque: only
// its order means anything, and a caller that invents one gets a refusal rather
// than a silently wrong page.
func parseSince(v string) (int64, error) {
	n, err := strconv.ParseInt(v, 10, 64)
	if err != nil || n < 0 {
		return 0, fmt.Errorf("since is a cursor a previous answer gave you, not %q", v)
	}
	return n, nil
}

// parseRef reads the pair a listing handed out. The box is not optional and is
// never inferred: an id alone names a row in each direction, and the archive
// spans both.
func parseRef(box, id string) (ref messageRef, err error) {
	if box != boxInbox && box != boxOutbox {
		return ref, fmt.Errorf("box is inbox or outbox, not %v", box)
	}
	if ref.ID, err = mcp.ParseMessageID(id); err != nil {
		return ref, err
	}
	ref.Box = box
	return ref, nil
}
