package mcp

// The message table is written by hand rather than by AutoMigrate, and every
// clause below is load-bearing.
//
// why the CHECKs are table-level and all three exist at creation: SQLite has no
// ALTER TABLE ADD CONSTRAINT, so a constraint missing here costs a full rebuild
// to add later. Without the two per-box checks, an UPDATE that names the wrong
// box succeeds: writing landed_at onto an inbox row is accepted with one row
// affected rather than refused.
//
// why owner is generated and not written: it is derivable from box and the two
// parties, so nothing should state it — and SQLite refuses to UPDATE a
// generated column, which is the guarantee a plain column cannot give. It is
// stored rather than virtual because every read scopes on it.
//
// why a unique index over (owner, box, id) rather than a primary key: a
// generated column may not sit in a PRIMARY KEY. The index gives the same
// guarantee, and OnConflict resolves against it, so a delivery that arrives
// twice is still stored once.
//
// why seq exists, and why the cursor is not a timestamp: created_at is read in
// Go before the INSERT, so a row can carry an earlier instant and commit later.
// A cursor over it therefore steps past a message that was not yet visible when
// the cursor was handed out, and that message is never answered again. Measured
// on this schema: four senders, a hundred messages twenty milliseconds apart,
// one reader paging — one to two messages lost per hundred, and far worse under
// load. seq is assigned by the database under the same lock as the row, so its
// order is commit order, which is the only order a reader can page.
//
// why (owner, box, id) and not (owner, id): an agent may hold two rows under
// one id — it sends to itself, or a peer mints an id this agent already sent
// under. The id is the peer's to choose, so neither case is refusable.
const ddlMessages = `
CREATE TABLE IF NOT EXISTS mcp__messages (
  seq        integer PRIMARY KEY AUTOINCREMENT,
  box        text NOT NULL,
  id         text NOT NULL,
  sender     text NOT NULL,
  recipient  text NOT NULL,
  owner      text GENERATED ALWAYS AS
               (CASE box WHEN 'inbox' THEN recipient ELSE sender END) STORED,

  content    text NOT NULL,
  parent_id  text,

  created_at  datetime NOT NULL,
  archived_at datetime,

  read_at           datetime,
  receipt_due_at    datetime,
  receipt_stored_at datetime,

  landed_at  datetime,
  failed_at  datetime,
  fetched_at datetime,
  err        text,

  CHECK (box IN ('inbox','outbox')),
  CHECK (box = 'outbox' OR (landed_at IS NULL AND failed_at IS NULL
                        AND fetched_at IS NULL AND err IS NULL)),
  CHECK (box = 'inbox'  OR (read_at IS NULL AND receipt_due_at IS NULL
                        AND receipt_stored_at IS NULL))
)`

// The indexes, each answering one read.
//
// why exactly one space before ON: the sqlite driver re-parses stored DDL to
// answer the migrator, and its index expression requires it. Aligned spacing
// makes ColumnTypes answer "invalid DDL".
//
// why the archive index is partial: `archived_at IS NOT NULL` plans as a range,
// so a plain (owner, archived_at, created_at) leaves nothing able to order and
// the read falls to a temp b-tree. The partial index puts the predicate in the
// index and orders on created_at, which is what the archive listing sorts by —
// the two have to name the same column or the sort lands in a temp b-tree
// anyway, which is what happened when they did not.
var ddlIndexes = []string{
	`CREATE UNIQUE INDEX IF NOT EXISTS ux_mcp__messages ON mcp__messages (owner, box, id)`,
	`CREATE INDEX IF NOT EXISTS ix_mcp__messages_box ON mcp__messages (owner, box, archived_at, seq)`,
	`CREATE INDEX IF NOT EXISTS ix_mcp__messages_parent ON mcp__messages (owner, parent_id, archived_at, created_at)`,
	`CREATE INDEX IF NOT EXISTS ix_mcp__messages_archive ON mcp__messages (owner, created_at) WHERE archived_at IS NOT NULL`,
}
