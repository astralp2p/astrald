package messaging

import (
	"errors"
	"reflect"
	"testing"
	"time"

	"github.com/astralp2p/astral-go/api/messaging"
	"github.com/astralp2p/astral-go/astral"
	"github.com/glebarez/sqlite"
	"gorm.io/gorm"
)

// The schema mod/mcp created before messaging left it, as a node at that
// revision holds it on disk.
var legacySchema = []string{
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
	"CREATE TABLE `mcp__agents` (`identity` text,`alias` text,`token` text,`expires_at` datetime,`created_at` datetime)",
	"CREATE UNIQUE INDEX `idx_mcp__agents_identity` ON `mcp__agents`(`identity`)",
	"CREATE UNIQUE INDEX `idx_mcp__agents_token` ON `mcp__agents`(`token`)",
}

// openEmptyDB opens an in-memory store with no table in it.
func openEmptyDB(t *testing.T) *DB {
	t.Helper()

	gdb, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	pool, err := gdb.DB()
	if err != nil {
		t.Fatalf("pool: %v", err)
	}
	pool.SetMaxOpenConns(1)

	return &DB{DB: gdb}
}

// seedLegacy writes a node's mail and agents the way mod/mcp stored them, and
// deletes the newest row so the sequence runs past every surviving seq. It
// answers the surviving rows, as written.
func seedLegacy(t *testing.T, db *DB, a, b *astral.Identity) []*dbMessage {
	t.Helper()

	for _, stmt := range legacySchema {
		if err := db.Exec(stmt).Error; err != nil {
			t.Fatalf("legacy schema: %v", err)
		}
	}

	rows := legacyMail(a, b)
	for _, row := range rows {
		if err := db.Table(legacyMessages).Create(row).Error; err != nil {
			t.Fatalf("legacy row: %v", err)
		}
	}
	dropped := rows[len(rows)-1]
	if err := db.Exec(`DELETE FROM mcp__messages WHERE seq = ?`, dropped.Seq).Error; err != nil {
		t.Fatalf("drop the newest row: %v", err)
	}

	for i, id := range []*astral.Identity{a, b} {
		err := db.Exec(`INSERT INTO mcp__agents (identity, alias, token, expires_at, created_at)
			VALUES (?, '', ?, ?, ?)`, id, i, legacyAt(0).AddDate(1, 0, 0), legacyAt(i)).Error
		if err != nil {
			t.Fatalf("legacy agent: %v", err)
		}
	}

	return rows[:len(rows)-1]
}

// legacyMail is a's mail as mod/mcp wrote it, through the row type mod/mcp
// wrote it with, so every column carries the driver's own encoding. Between them
// the rows set every column: a question read and owed a receipt that went out;
// the answer, landed and collected; a refusal with empty words and one with
// words, put away; and a newest message for seedLegacy to delete.
//
// why a root names the zero id and not NULL: MessageID writes its hex form
// whatever its value, so no row mod/mcp wrote holds a NULL parent.
func legacyMail(a, b *astral.Identity) []*dbMessage {
	empty, words := "", "no"
	ask := messaging.NewMessageID()

	return []*dbMessage{
		{Box: messaging.BoxInbox, ID: ask, Sender: b, Recipient: a, Content: "ask", CreatedAt: *legacyAt(0),
			ReadAt: legacyAt(1), ReceiptDueAt: legacyAt(1), ReceiptStoredAt: legacyAt(2)},
		{Box: messaging.BoxOutbox, ID: messaging.NewMessageID(), Sender: a, Recipient: b, Content: "answer",
			ParentID: ask, CreatedAt: *legacyAt(3), LandedAt: legacyAt(3), FetchedAt: legacyAt(4)},
		{Box: messaging.BoxOutbox, ID: messaging.NewMessageID(), Sender: a, Recipient: b, Content: "turned away",
			CreatedAt: *legacyAt(5), FailedAt: legacyAt(5), Err: &empty},
		{Box: messaging.BoxOutbox, ID: messaging.NewMessageID(), Sender: a, Recipient: b, Content: "refused",
			CreatedAt: *legacyAt(6), ArchivedAt: legacyAt(9), FailedAt: legacyAt(6), Err: &words},
		{Box: messaging.BoxInbox, ID: messaging.NewMessageID(), Sender: b, Recipient: a, Content: "dropped",
			CreatedAt: *legacyAt(7)},
	}
}

// legacyAt is an instant the legacy rows were written at, minutes into it.
func legacyAt(minutes int) *time.Time {
	at := time.Date(2026, 1, 1, 10, minutes, 0, 0, time.UTC)
	return &at
}

// checkEveryColumnSet asserts that some row sets each column, so a comparison
// over the rows covers every column.
func checkEveryColumnSet(t *testing.T, rows []map[string]any) {
	t.Helper()

	for column := range rows[0] {
		set := false
		for _, row := range rows {
			set = set || row[column] != nil
		}
		if !set {
			t.Fatalf("no legacy row sets %v, so its carry-over goes unchecked", column)
		}
	}
}

// messageRows answers every row of a mail table, every column, in seq order.
func messageRows(t *testing.T, db *DB, table string) []map[string]any {
	t.Helper()

	var rows []map[string]any
	err := db.Raw(`SELECT seq, box, id, sender, recipient, owner, content, parent_id,
		created_at, archived_at, read_at, receipt_due_at, receipt_stored_at,
		landed_at, failed_at, fetched_at, err
		FROM ` + table + ` ORDER BY seq`).Scan(&rows).Error
	if err != nil {
		t.Fatalf("rows of %v: %v", table, err)
	}
	return rows
}

// sequenceOf answers the AUTOINCREMENT high-water mark of a table.
func sequenceOf(t *testing.T, db *DB, table string) int64 {
	t.Helper()

	var seq int64
	if err := db.Raw(`SELECT seq FROM sqlite_sequence WHERE name = ?`, table).Scan(&seq).Error; err != nil {
		t.Fatalf("sequence of %v: %v", table, err)
	}
	return seq
}

// A node upgraded from the revision where mod/mcp held the mail keeps every
// message as it stood — seq, id, box, parties, stamps and the difference
// between no refusal and an empty one — and the cursor sequence with it, so no
// new message is written under a seq an inbox already paged past.
func TestTheLegacyMailAndAgentsCarryOver(t *testing.T) {
	db := openEmptyDB(t)
	a, b := astral.GenerateIdentity(), astral.GenerateIdentity()
	seedLegacy(t, db, a, b)

	before := messageRows(t, db, legacyMessages)
	checkEveryColumnSet(t, before)
	legacySeq := sequenceOf(t, db, legacyMessages)

	if err := db.Migrate(); err != nil {
		t.Fatalf("migrate: %v", err)
	}

	if old, _ := hasTable(db.DB, legacyMessages); old {
		t.Fatal("the legacy mail table is still there")
	}
	if after := messageRows(t, db, tableMessages); !reflect.DeepEqual(before, after) {
		t.Fatalf("the rows changed on the way over:\nbefore %v\nafter  %v", before, after)
	}
	if seq := sequenceOf(t, db, tableMessages); seq != legacySeq {
		t.Fatalf("sequence %v, want the legacy %v", seq, legacySeq)
	}

	checkIndexes(t, db)

	mustMigrateIdentically(t, db, before)

	rows, err := db.ListMailboxes()
	if err != nil || len(rows) != 2 {
		t.Fatalf("mailboxes carried over: %v, err %v; want both agents", len(rows), err)
	}
	for _, row := range rows {
		if row.ContractID != nil || row.ExpiresAt != nil {
			t.Fatalf("mailbox %v was imported naming contract %v; want it pending", row.Identity, row.ContractID)
		}
	}

	next := &messaging.StoredMessage{ID: messaging.NewMessageID(), Sender: b, Recipient: a, Content: "new"}
	if _, err = db.InsertInbox(next); err != nil {
		t.Fatalf("insert after the upgrade: %v", err)
	}
	if seq := sequenceOf(t, db, tableMessages); seq != legacySeq+1 {
		t.Fatalf("a new row took seq %v, want %v", seq, legacySeq+1)
	}
}

// checkIndexes asserts the mail table carries the current indexes and none of
// the ones named after the legacy table.
func checkIndexes(t *testing.T, db *DB) {
	t.Helper()

	var names []string
	err := db.Raw(`SELECT name FROM sqlite_master WHERE type = 'index' AND tbl_name = ? AND sql IS NOT NULL`, tableMessages).
		Scan(&names).Error
	if err != nil {
		t.Fatalf("indexes: %v", err)
	}

	want := map[string]bool{
		"ux_messaging__messages":         true,
		"ix_messaging__messages_box":     true,
		"ix_messaging__messages_parent":  true,
		"ix_messaging__messages_archive": true,
		"ix_messaging__messages_unread":  true,
		"ix_messaging__messages_pickup":  true,
	}
	if len(names) != len(want) {
		t.Fatalf("indexes %v, want exactly %v", names, want)
	}
	for _, name := range names {
		if !want[name] {
			t.Fatalf("index %v is not one of the current ones", name)
		}
	}
}

// mustMigrateIdentically runs the migration again and asserts it changed
// nothing.
func mustMigrateIdentically(t *testing.T, db *DB, rows []map[string]any) {
	t.Helper()

	if err := db.Migrate(); err != nil {
		t.Fatalf("a second migration: %v", err)
	}
	after := messageRows(t, db, tableMessages)
	if len(rows)+len(after) > 0 && !reflect.DeepEqual(rows, after) {
		t.Fatalf("a second migration changed the rows:\nbefore %v\nafter  %v", rows, after)
	}
	checkIndexes(t, db)
}

// The agents are carried over once. An identity deleted afterwards stays
// deleted across a restart, though its mcp row is still there.
func TestADeletedParticipantIsNotCarriedOverAgain(t *testing.T) {
	db := openEmptyDB(t)
	a, b := astral.GenerateIdentity(), astral.GenerateIdentity()
	seedLegacy(t, db, a, b)

	if err := db.Migrate(); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	if err := db.DeleteMailbox(a); err != nil {
		t.Fatalf("delete mailbox: %v", err)
	}
	if err := db.Migrate(); err != nil {
		t.Fatalf("migrate again: %v", err)
	}

	if _, err := db.FindMailbox(a); err == nil {
		t.Fatal("a deleted participant's mailbox was imported again from its mcp row")
	}
	if _, err := db.FindMailbox(b); err != nil {
		t.Fatalf("the other participant: %v", err)
	}
}

// A node that never ran mod/mcp gets the module's tables and nothing named
// after the legacy ones.
func TestAFreshNodeGetsOnlyTheCurrentTables(t *testing.T) {
	db := openEmptyDB(t)

	if err := db.Migrate(); err != nil {
		t.Fatalf("migrate: %v", err)
	}

	var tables []string
	if err := db.Raw(`SELECT name FROM sqlite_master WHERE type = 'table' AND name NOT LIKE 'sqlite_%' ORDER BY name`).Scan(&tables).Error; err != nil {
		t.Fatalf("tables: %v", err)
	}
	if !reflect.DeepEqual(tables, []string{tableMailboxes, tableMessages}) {
		t.Fatalf("tables %v, want only %v and %v", tables, tableMailboxes, tableMessages)
	}
	checkIndexes(t, db)

	mustMigrateIdentically(t, db, nil)
}

// The in-memory index is loaded from the store, so a mailbox hosted before a
// restart is hosted after it, and not before the load.
func TestTheIndexIsLoadedFromTheStore(t *testing.T) {
	mod := testMessagingModule(t)
	identity := hostedParticipant(t, mod)

	restarted := &Module{Deps: mod.Deps, ctx: mod.ctx, db: mod.db, node: mod.node, config: mod.config, log: mod.log}
	if restarted.hosts(identity) {
		t.Fatal("the index answered before it was loaded")
	}

	if err := restarted.loadMailboxes(); err != nil {
		t.Fatalf("load mailboxes: %v", err)
	}
	if !restarted.hosts(identity) {
		t.Fatal("a stored mailbox is not hosted after a load")
	}
}

// The first start after the upgrade provisions a hosting contract for every
// imported mailbox whose key the node holds, and records it. Until then an
// imported mailbox is pending and not served. A mailbox whose key the node
// does not hold stays pending and unserved. A second run changes nothing.
func TestRunProvisionsTheImportedMailboxes(t *testing.T) {
	db := openEmptyDB(t)
	mod := testModuleOver(t, db)
	held, foreign := keysOf(mod).mint(), astral.GenerateIdentity()
	seedLegacy(t, db, held, foreign)

	if err := db.Migrate(); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	if err := mod.loadMailboxes(); err != nil {
		t.Fatalf("load mailboxes: %v", err)
	}
	if mod.hosts(held) || mod.hosts(foreign) {
		t.Fatal("a pending mailbox is served before Run provisioned it")
	}

	runUntil(t, mod, func() bool { return mod.hosts(held) })
	first := mustFindMailbox(t, db, held)

	if !mod.hosts(held) || first.ContractID == nil {
		t.Fatal("the imported mailbox whose key the node holds is not hosted after Run")
	}
	if row := mustFindMailbox(t, db, foreign); mod.hosts(foreign) || row.ContractID != nil {
		t.Fatal("a mailbox whose key the node does not hold was provisioned")
	}

	runUntil(t, mod, func() bool { return true })

	if again := mustFindMailbox(t, db, held); !again.ContractID.IsEqual(first.ContractID) {
		t.Fatalf("a second run replaced contract %v with %v", first.ContractID, again.ContractID)
	}
	if n := len(hostingContracts(t, mod, held)); n != 1 {
		t.Fatalf("%v hosting contracts after two runs, want 1", n)
	}
}

// runUntil runs the module as a node does until done answers true, then stops
// it and waits for Run to return, so whatever Run started has finished.
func runUntil(t *testing.T, mod *Module, done func() bool) {
	t.Helper()

	ctx, cancel := mod.ctx.WithCancel()
	defer cancel()

	returned := make(chan error, 1)
	go func() { returned <- mod.Run(ctx) }()

	deadline := time.Now().Add(3 * time.Second)
	for !done() {
		if time.Now().After(deadline) {
			t.Fatal("Run did not reach the state the test waits for")
		}
		time.Sleep(5 * time.Millisecond)
	}

	cancel()
	if err := <-returned; err != nil {
		t.Fatalf("run: %v", err)
	}
}

// mustFindMailbox answers the identity's index row.
func mustFindMailbox(t *testing.T, db *DB, identity *astral.Identity) *dbMailbox {
	t.Helper()

	row, err := db.FindMailbox(identity)
	if err != nil {
		t.Fatalf("index row of %v: %v", identity, err)
	}
	return row
}

// A provisioning run records a contract only on a row still pending. A row
// provisioned meanwhile keeps its contract, and a row deleted meanwhile is not
// brought back into the index.
func TestProvisioningRecordsOnlyOnAPendingRow(t *testing.T) {
	mod := testMessagingModule(t)
	provisioned, deleted := hostedParticipant(t, mod), keysOf(mod).mint()
	kept, _ := mod.mailboxes.Get(provisioned.String())

	late, err := mod.signHosting(mod.ctx, provisioned)
	if err != nil {
		t.Fatalf("sign hosting: %v", err)
	}
	if err = mod.recordHosting(provisioned, late); !errors.Is(err, errNotPending) {
		t.Fatalf("record hosting on a provisioned row: got %v, want %v", err, errNotPending)
	}
	if row := mustFindMailbox(t, mod.db, provisioned); !row.ContractID.IsEqual(kept.ContractID) {
		t.Fatalf("a provisioned row now names %v, want its own %v", row.ContractID, kept.ContractID)
	}

	if err = mod.recordHosting(deleted, late); !errors.Is(err, errNotPending) {
		t.Fatalf("record hosting on a deleted row: got %v, want %v", err, errNotPending)
	}

	// what a provisioning run reads as a skip and not as a mailbox provisioned
	if err = mod.provisionMailbox(mod.ctx, deleted); !errors.Is(err, errNotPending) {
		t.Fatalf("provisioning a deleted row: got %v, want %v", err, errNotPending)
	}
	if _, ok := mod.mailboxes.Get(deleted.String()); ok {
		t.Fatal("a row the table does not hold was mirrored into the index")
	}
}
