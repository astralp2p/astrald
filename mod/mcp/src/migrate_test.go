package mcp

import (
	"reflect"
	"testing"

	"github.com/astralp2p/astral-go/astral"
	messagingsrc "github.com/astralp2p/astrald/mod/messaging/src"
	"gorm.io/gorm"
)

// legacyMail is the mail table and its indexes as mod/mcp created them before
// mail moved to mod/messaging, as a node at that revision holds them on disk.
var legacyMail = []string{
	`CREATE TABLE mcp__messages (
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
)`,
	`CREATE UNIQUE INDEX ux_mcp__messages ON mcp__messages (owner, box, id)`,
	`CREATE INDEX ix_mcp__messages_box ON mcp__messages (owner, box, archived_at, seq)`,
	`CREATE INDEX ix_mcp__messages_parent ON mcp__messages (owner, parent_id, archived_at, created_at)`,
	`CREATE INDEX ix_mcp__messages_archive ON mcp__messages (owner, created_at) WHERE archived_at IS NOT NULL`,
	`CREATE INDEX ix_mcp__messages_unread ON mcp__messages (owner, box, archived_at, seq) WHERE read_at IS NULL`,
	`CREATE INDEX ix_mcp__messages_pickup ON mcp__messages (owner, box, archived_at, seq) WHERE landed_at IS NOT NULL AND fetched_at IS NULL`,
}

// A fresh node's mcp migration creates the agent table and nothing else: the
// mail is mod/messaging's, and so are its tables.
func TestMigrateCreatesNoMailTable(t *testing.T) {
	db := &DB{DB: emptyStore(t)}

	if err := db.Migrate(); err != nil {
		t.Fatalf("migrate: %v", err)
	}

	if tables := tablesOf(t, db.DB); !reflect.DeepEqual(tables, []string{"mcp__agents"}) {
		t.Fatalf("tables %v, want only mcp__agents", tables)
	}
}

// On a node upgraded from the revision where mod/mcp held the mail, the mcp
// migration leaves the legacy mail as it lies — table, indexes, rows and
// sequence — so mod/messaging carries it over whichever module migrates
// first; and once messaging has, no mcp migration brings the old table back.
func TestMigrateLeavesTheLegacyMailToMessaging(t *testing.T) {
	store := emptyStore(t)
	seedLegacyMail(t, store)
	before := schemaOf(t, store)

	mustMigrateMCP(t, store)
	if after := schemaOf(t, store); !reflect.DeepEqual(before, after) {
		t.Fatalf("the mcp migration changed the store:\nbefore %v\nafter  %v", before, after)
	}

	if err := (&messagingsrc.DB{DB: store}).Migrate(); err != nil {
		t.Fatalf("messaging migrate: %v", err)
	}
	mustMigrateMCP(t, store)

	tables := tablesOf(t, store)
	want := []string{"mcp__agents", "messaging__mailboxes", "messaging__messages"}
	if !reflect.DeepEqual(tables, want) {
		t.Fatalf("tables %v after both migrations, want %v", tables, want)
	}
	var carried int64
	if err := store.Raw(`SELECT count(*) FROM messaging__messages`).Scan(&carried).Error; err != nil || carried != 2 {
		t.Fatalf("messaging holds %v carried-over messages, err %v; want 2", carried, err)
	}
}

// seedLegacyMail writes the store a node at the legacy revision holds: the
// agent table, and the mail table with two rows under a sequence that has run
// past both.
func seedLegacyMail(t *testing.T, store *gorm.DB) {
	t.Helper()

	// why the current migration writes the agent table: the agent row type is
	// the one the legacy revision migrated.
	mustMigrateMCP(t, store)

	for _, stmt := range legacyMail {
		if err := store.Exec(stmt).Error; err != nil {
			t.Fatalf("legacy schema: %v", err)
		}
	}

	a, b := astral.GenerateIdentity(), astral.GenerateIdentity()
	for _, row := range [][]any{{"inbox", "01", b, a}, {"outbox", "02", a, b}, {"inbox", "03", b, a}} {
		err := store.Exec(`INSERT INTO mcp__messages (box, id, sender, recipient, content, created_at)
			VALUES (?, ?, ?, ?, 'x', '2026-01-01 00:00:00')`, row...).Error
		if err != nil {
			t.Fatalf("legacy row: %v", err)
		}
	}
	if err := store.Exec(`DELETE FROM mcp__messages WHERE id = '03'`).Error; err != nil {
		t.Fatalf("drop the newest row: %v", err)
	}
}

func mustMigrateMCP(t *testing.T, store *gorm.DB) {
	t.Helper()

	if err := (&DB{DB: store}).Migrate(); err != nil {
		t.Fatalf("mcp migrate: %v", err)
	}
}

// tablesOf answers the store's own tables, by name.
func tablesOf(t *testing.T, store *gorm.DB) []string {
	t.Helper()

	var tables []string
	err := store.Raw(`SELECT name FROM sqlite_master WHERE type = 'table' AND name NOT LIKE 'sqlite_%' ORDER BY name`).
		Scan(&tables).Error
	if err != nil {
		t.Fatalf("tables: %v", err)
	}
	return tables
}

// schemaOf answers what the mcp migration must leave alone: every schema
// object but the agent table's own, every mail row and the sequences.
//
// why the agent table is left out: gorm's sqlite migrator rebuilds it on every
// run, which rewrites its stored statement and changes none of its rows.
func schemaOf(t *testing.T, store *gorm.DB) map[string][]map[string]any {
	t.Helper()

	out := map[string][]map[string]any{}
	for name, stmt := range map[string]string{
		"schema":   `SELECT type, name, sql FROM sqlite_master WHERE tbl_name <> 'mcp__agents' ORDER BY name`,
		"mail":     `SELECT * FROM mcp__messages ORDER BY seq`,
		"sequence": `SELECT * FROM sqlite_sequence ORDER BY name`,
	} {
		var rows []map[string]any
		if err := store.Raw(stmt).Scan(&rows).Error; err != nil {
			t.Fatalf("%v: %v", name, err)
		}
		out[name] = rows
	}
	return out
}
