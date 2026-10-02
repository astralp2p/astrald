package messaging

import (
	"fmt"

	"gorm.io/gorm"
)

// Revisions, the peer column and the conversation summaries.
//
// A revision is a position in one node-wide order of changes. Every write that
// changes what a row or a conversation answers takes the next one, so a caller
// holding the greatest revision it has seen asks for what changed after it and
// misses nothing: an arrival, a fate, a read, an archive and its undo are all
// one kind of event.
//
// why one allocator for rows and conversations: a revision is then unique
// across both, and a caller holding one cursor per stream never sees two
// changes under one value.
//
// why triggers and not the writers: every statement that writes a message row
// takes a revision without being named here, so a stamp added later cannot
// forget one. A column added to the envelope must be added to envelopeColumns.
const (
	tableRevisions     = "messaging__revisions"
	tableConversations = "messaging__conversations"
	tableGenerations   = "messaging__generations"
)

// mutableColumns are the envelope columns a write may change. A change to any
// of them is a change a caller must see.
var mutableColumns = []string{
	"parent_id", "created_at", "archived_at",
	"read_at", "receipt_due_at", "receipt_stored_at",
	"landed_at", "failed_at", "fetched_at", "err",
}

// fixedColumns never change once written. owner and peer are generated from
// them, so a row never moves between conversations.
var fixedColumns = []string{"box", "id", "sender", "recipient", "content"}

// ddlRevisions creates what the revision order keeps beside the messages.
//
// why the CHECK on typeof: SQLite answers an integer overflow as a REAL, and a
// revision that stopped being an integer would stop being an order. The CHECK
// fails the write instead.
var ddlRevisions = []string{
	`CREATE TABLE IF NOT EXISTS messaging__revisions (
  id  integer PRIMARY KEY CHECK (id = 1),
  rev integer NOT NULL CHECK (typeof(rev) = 'integer' AND rev >= 0)
)`,
	// latest_seq is null for a tombstone: a peer whose last unarchived message
	// was put away. The row stays, so a caller following changes sees it go.
	`CREATE TABLE IF NOT EXISTS messaging__conversations (
  owner      text NOT NULL,
  peer       text NOT NULL,
  latest_seq integer,
  latest_rev integer,
  unread     integer NOT NULL CHECK (unread >= 0),
  rev        integer NOT NULL,
  PRIMARY KEY (owner, peer)
)`,
	// generation answers whether a cursor still names this mailbox's rows. A
	// deleted mailbox takes a new one, and an absent row reads as zero.
	`CREATE TABLE IF NOT EXISTS messaging__generations (
  owner      text PRIMARY KEY,
  generation integer NOT NULL
)`,
}

// ddlRevisionIndexes answer the page and change reads.
//
// why peer leads with archived_at before seq: a conversation's transcript is
// its unarchived rows in seq order, and the latest one is a seek to the end.
// why the unread index is partial: the count it answers is kept incrementally;
// the index serves the backfill and the repair, never the write path.
var ddlRevisionIndexes = []string{
	`CREATE INDEX IF NOT EXISTS ix_messaging__messages_peer ON messaging__messages (owner, peer, archived_at, seq)`,
	`CREATE INDEX IF NOT EXISTS ix_messaging__messages_rev ON messaging__messages (owner, rev)`,
	`CREATE INDEX IF NOT EXISTS ix_messaging__messages_peer_rev ON messaging__messages (owner, peer, rev)`,
	`CREATE INDEX IF NOT EXISTS ix_messaging__messages_archived_seq ON messaging__messages (owner, seq) WHERE archived_at IS NOT NULL`,
	`CREATE INDEX IF NOT EXISTS ix_messaging__conversations_latest ON messaging__conversations (owner, latest_seq) WHERE latest_seq IS NOT NULL`,
	`CREATE INDEX IF NOT EXISTS ix_messaging__conversations_rev ON messaging__conversations (owner, rev)`,
}

// nextRev is the statement every trigger advances the allocator with, and
// currentRev the value it took.
const (
	nextRev    = `UPDATE messaging__revisions SET rev = rev + 1 WHERE id = 1;`
	currentRev = `(SELECT rev FROM messaging__revisions WHERE id = 1)`
)

// unreadOf counts one row toward its conversation's unread: an inbox row whose
// body was never handed out and that was not put away.
func unreadOf(row string) string {
	return fmt.Sprintf(`(%[1]s.box = 'inbox' AND %[1]s.read_at IS NULL AND %[1]s.archived_at IS NULL)`, row)
}

// latestOf answers one column of a conversation's newest unarchived row.
func latestOf(col string) string {
	return fmt.Sprintf(`(SELECT m.%s FROM messaging__messages m
    WHERE m.owner = NEW.owner AND m.peer = NEW.peer AND m.archived_at IS NULL
    ORDER BY m.seq DESC LIMIT 1)`, col)
}

// ddlTriggers keep revisions and summaries with every write.
//
// The row takes its revision first, so the summary that follows reads the
// row's new one as latest_rev rather than the value NEW carried.
//
// why the summary takes a revision only when it changed: a stamp on an old row
// changes nothing the conversation list draws, and a revision for it would make
// a follower re-read a row that did not move. The allocator still advances,
// and a gap in the order is no loss.
//
// why recursion cannot happen: the inner UPDATE sets rev alone, and rev is in
// no OF list here.
func ddlTriggers() []string {
	return []string{triggerFixed(), triggerInsert(), triggerUpdate()}
}

// anyChanged answers whether any of cols differs between OLD and NEW, null-safe.
func anyChanged(cols []string) string {
	s := ""
	for i, c := range cols {
		if i > 0 {
			s += " OR "
		}
		s += fmt.Sprintf("OLD.%[1]s IS NOT NEW.%[1]s", c)
	}
	return s
}

// stampRow gives the written row the next revision, then advances the
// allocator once more for the conversation that may follow.
const stampRow = nextRev + `
  UPDATE messaging__messages SET rev = ` + currentRev + ` WHERE seq = NEW.seq;
  ` + nextRev

func triggerFixed() string {
	return `CREATE TRIGGER IF NOT EXISTS tr_messaging__messages_fixed
BEFORE UPDATE ON messaging__messages
WHEN ` + anyChanged(fixedColumns) + `
BEGIN
  SELECT RAISE(ABORT, 'a message row never changes its box, id, parties or content');
END`
}

func triggerInsert() string {
	return `CREATE TRIGGER IF NOT EXISTS tr_messaging__messages_insert
AFTER INSERT ON messaging__messages
BEGIN
  ` + stampRow + `
  INSERT INTO messaging__conversations (owner, peer, latest_seq, latest_rev, unread, rev)
    VALUES (NEW.owner, NEW.peer, NULL, NULL, 0, 0)
    ON CONFLICT (owner, peer) DO NOTHING;
  UPDATE messaging__conversations SET
    unread     = unread + ` + unreadOf("NEW") + `,
    latest_seq = ` + latestOf("seq") + `,
    latest_rev = ` + latestOf("rev") + `,
    rev        = ` + currentRev + `
  WHERE owner = NEW.owner AND peer = NEW.peer;
END`
}

func triggerUpdate() string {
	return `CREATE TRIGGER IF NOT EXISTS tr_messaging__messages_update
AFTER UPDATE OF ` + joinColumns(mutableColumns) + ` ON messaging__messages
WHEN ` + anyChanged(mutableColumns) + `
BEGIN
  ` + stampRow + `
  UPDATE messaging__conversations SET
    unread     = unread + ` + unreadOf("NEW") + ` - ` + unreadOf("OLD") + `,
    latest_seq = ` + latestOf("seq") + `,
    latest_rev = ` + latestOf("rev") + `,
    rev        = ` + currentRev + `
  WHERE owner = NEW.owner AND peer = NEW.peer
    AND (latest_seq IS NOT ` + latestOf("seq") + `
      OR latest_rev IS NOT ` + latestOf("rev") + `
      OR ` + unreadOf("NEW") + ` != ` + unreadOf("OLD") + `);
END`
}

func joinColumns(cols []string) string {
	s := ""
	for i, c := range cols {
		if i > 0 {
			s += ", "
		}
		s += c
	}
	return s
}

// migrateRevisions brings a store of any age to revisions, peers and summaries,
// in one transaction: a start that fails halfway leaves the store as it found
// it, and the next start runs the whole step again.
//
// The allocator's row is the mark that the step ran. A store that holds it is
// only checked for its indexes and triggers: the counter is never reset and the
// summaries are never rebuilt on a restart.
func (db *DB) migrateRevisions() error {
	return db.Transaction(func(tx *gorm.DB) error {
		if err := addColumn(tx, "rev", `ALTER TABLE messaging__messages ADD COLUMN rev integer NOT NULL DEFAULT 0`); err != nil {
			return err
		}
		// why VIRTUAL: SQLite adds a generated column to an existing table only
		// as VIRTUAL, and an index over it stores the value all the same.
		if err := addColumn(tx, "peer", `ALTER TABLE messaging__messages ADD COLUMN peer text
  GENERATED ALWAYS AS (CASE box WHEN 'inbox' THEN sender ELSE recipient END) VIRTUAL`); err != nil {
			return err
		}
		if err := execAll(tx, ddlRevisions); err != nil {
			return err
		}

		var marked int64
		if err := tx.Raw(`SELECT count(*) FROM messaging__revisions`).Scan(&marked).Error; err != nil {
			return err
		}
		if marked == 0 {
			if err := backfillRevisions(tx); err != nil {
				return err
			}
		}

		if err := execAll(tx, ddlRevisionIndexes); err != nil {
			return err
		}
		return execAll(tx, ddlTriggers())
	})
}

// backfillRevisions gives every existing row and conversation a revision.
//
// A row takes its seq: seq is already a unique order below the allocator's
// start. Each summary takes a revision of its own above every row's, so the
// summary stream has no two equal values either.
func backfillRevisions(tx *gorm.DB) error {
	return execAll(tx, []string{
		`UPDATE messaging__messages SET rev = seq`,
		`INSERT INTO messaging__revisions (id, rev) SELECT 1, COALESCE(MAX(seq), 0) FROM messaging__messages`,
		`DELETE FROM messaging__conversations`,
		`INSERT INTO messaging__conversations (owner, peer, latest_seq, latest_rev, unread, rev)
  SELECT owner, peer,
    MAX(CASE WHEN archived_at IS NULL THEN seq END),
    MAX(CASE WHEN archived_at IS NULL THEN seq END),
    SUM(` + unreadOf("messaging__messages") + `),
    0
  FROM messaging__messages
  GROUP BY owner, peer`,
		`UPDATE messaging__conversations SET rev = ` + currentRev + ` + rowid`,
		`UPDATE messaging__revisions SET rev = rev + (SELECT COALESCE(MAX(rowid), 0) FROM messaging__conversations) WHERE id = 1`,
	})
}

// addColumn adds a column the message table lacks.
//
// why table_xinfo: table_info omits generated columns, so peer would read as
// absent on every start and the ALTER would fail the second time.
func addColumn(tx *gorm.DB, name, ddl string) error {
	var n int64
	err := tx.Raw(`SELECT count(*) FROM pragma_table_xinfo('messaging__messages') WHERE name = ?`, name).
		Scan(&n).Error
	if err != nil || n > 0 {
		return err
	}
	return tx.Exec(ddl).Error
}
