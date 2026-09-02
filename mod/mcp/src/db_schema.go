package mcp

// ddlMessages is written by hand: a generated column, three CHECKs and a
// partial index are none of them expressible as a struct tag, and SQLite has no
// ALTER TABLE ADD CONSTRAINT, so all three CHECKs exist at creation.
//
// why owner is generated and keyed by a unique index: SQLite refuses to UPDATE
// a generated column and refuses one in a PRIMARY KEY. The index gives the same
// guarantee and OnConflict resolves against it. It carries box because one agent
// may hold two rows under one id, sending to itself or receiving an id it sent.
//
// why seq and not created_at: created_at is read in Go before the INSERT, so a
// row can carry an earlier instant and commit later, and a cursor over it steps
// past that row permanently.
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
// why the archive index is partial and orders on created_at: archived_at IS NOT
// NULL plans as a range, so a plain index leaves nothing able to order and the
// sort falls to a temp b-tree unless the index names the column the listing
// sorts by.
var ddlIndexes = []string{
	`CREATE UNIQUE INDEX IF NOT EXISTS ux_mcp__messages ON mcp__messages (owner, box, id)`,
	`CREATE INDEX IF NOT EXISTS ix_mcp__messages_box ON mcp__messages (owner, box, archived_at, seq)`,
	`CREATE INDEX IF NOT EXISTS ix_mcp__messages_parent ON mcp__messages (owner, parent_id, archived_at, created_at)`,
	`CREATE INDEX IF NOT EXISTS ix_mcp__messages_archive ON mcp__messages (owner, created_at) WHERE archived_at IS NOT NULL`,
}
