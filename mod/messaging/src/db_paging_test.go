package messaging

import (
	"strings"
	"testing"
	"time"

	"github.com/astralp2p/astral-go/api/messaging"
	"github.com/astralp2p/astral-go/astral"
	"github.com/glebarez/sqlite"
	"gorm.io/gorm"
)

// inbox stores a message b wrote to a and answers its id.
func inbox(t *testing.T, db *DB, from, to *astral.Identity) messaging.MessageID {
	t.Helper()
	id := messaging.NewMessageID()
	if _, err := db.InsertInbox(&messaging.StoredMessage{ID: id, Sender: from, Recipient: to, Content: "body"}); err != nil {
		t.Fatalf("insert inbox: %v", err)
	}
	return id
}

// outbox stores a message from wrote to to and answers its id.
func outbox(t *testing.T, db *DB, from, to *astral.Identity) messaging.MessageID {
	t.Helper()
	id := messaging.NewMessageID()
	if err := db.InsertOutbox(&messaging.StoredMessage{ID: id, Sender: from, Recipient: to, Content: "body"}); err != nil {
		t.Fatalf("insert outbox: %v", err)
	}
	return id
}

// revOf answers the revision a row holds now.
func revOf(t *testing.T, db *DB, owner *astral.Identity, box string, id messaging.MessageID) int64 {
	t.Helper()
	var rev int64
	err := db.Raw(`SELECT rev FROM messaging__messages WHERE owner = ? AND box = ? AND id = ?`, owner, box, id).
		Scan(&rev).Error
	if err != nil {
		t.Fatalf("rev: %v", err)
	}
	return rev
}

// conversation answers one summary by peer, failing when it is absent.
func conversation(t *testing.T, db *DB, owner, peer *astral.Identity) dbConversation {
	t.Helper()
	page, err := db.PageConversations(owner, peer, 0, 100)
	if err != nil {
		t.Fatalf("conversation: %v", err)
	}
	if len(page.Conversations) != 1 {
		t.Fatalf("conversation with %v: %v rows, want 1", peer, len(page.Conversations))
	}
	return page.Conversations[0]
}

func TestEveryWriteTakesANewRevision(t *testing.T) {
	db := testDB(t)
	a, b := astral.GenerateIdentity(), astral.GenerateIdentity()

	id := outbox(t, db, a, b)
	seen := map[int64]bool{}
	step := func(name string) {
		t.Helper()
		rev := revOf(t, db, a, messaging.BoxOutbox, id)
		if rev == 0 || seen[rev] {
			t.Fatalf("%v: rev %v is zero or repeated", name, rev)
		}
		seen[rev] = true
	}
	step("insert")

	for _, w := range []struct {
		name string
		fn   func() error
	}{
		{"landed", func() error { return db.StampLanded(a, id) }},
		{"fetched", func() error { return db.StampFetched(a, id) }},
		{"archive", func() error { _, err := db.Archive(a, messaging.BoxOutbox, id); return err }},
		{"undo", func() error { _, err := db.Unarchive(a, messaging.BoxOutbox, id); return err }},
	} {
		if err := w.fn(); err != nil {
			t.Fatalf("%v: %v", w.name, err)
		}
		step(w.name)
	}

	got := map[string]int64{}
	in := inbox(t, db, b, a)
	for _, w := range []struct {
		name string
		fn   func() error
	}{
		{"read", func() error {
			return db.MarkRead(a, &messaging.StoredMessage{ID: in, Box: messaging.BoxInbox})
		}},
		{"receipt due", func() error { _, err := db.MarkReceiptDue(a, in); return err }},
		{"receipt stored", func() error { return db.StampReceiptStored(a, in) }},
	} {
		before := revOf(t, db, a, messaging.BoxInbox, in)
		if err := w.fn(); err != nil {
			t.Fatalf("%v: %v", w.name, err)
		}
		got[w.name] = revOf(t, db, a, messaging.BoxInbox, in)
		if got[w.name] <= before {
			t.Fatalf("%v: rev %v did not move past %v", w.name, got[w.name], before)
		}
	}
}

// A statement that changes nothing a caller sees takes no revision: a stamp
// written once is not written again.
func TestAWriteThatChangesNothingTakesNoRevision(t *testing.T) {
	db := testDB(t)
	a, b := astral.GenerateIdentity(), astral.GenerateIdentity()
	id := outbox(t, db, a, b)
	if err := db.StampLanded(a, id); err != nil {
		t.Fatal(err)
	}
	before := revOf(t, db, a, messaging.BoxOutbox, id)

	err := db.Exec(`UPDATE messaging__messages SET landed_at = landed_at WHERE owner = ? AND id = ?`, a, id).Error
	if err != nil {
		t.Fatal(err)
	}
	if after := revOf(t, db, a, messaging.BoxOutbox, id); after != before {
		t.Fatalf("a no-op update moved rev %v to %v", before, after)
	}
}

func TestARowNeverMovesBetweenConversations(t *testing.T) {
	db := testDB(t)
	a, b, c := astral.GenerateIdentity(), astral.GenerateIdentity(), astral.GenerateIdentity()
	id := inbox(t, db, b, a)

	err := db.Exec(`UPDATE messaging__messages SET sender = ? WHERE owner = ? AND id = ?`, c, a, id).Error
	if err == nil || !strings.Contains(err.Error(), "never changes") {
		t.Fatalf("moving a row's sender answered %v, want the refusal", err)
	}
}

func TestAPageEndsExactlyWhereTheRowsDo(t *testing.T) {
	db := testDB(t)
	a, b := astral.GenerateIdentity(), astral.GenerateIdentity()

	empty, err := db.PageMessages(a, pageQuery{List: messaging.ListInbox, Limit: 3})
	if err != nil || len(empty.Rows) != 0 || empty.Next != 0 {
		t.Fatalf("empty inbox: %v rows, next %v, err %v", len(empty.Rows), empty.Next, err)
	}

	for i := 0; i < 6; i++ {
		inbox(t, db, b, a)
	}

	var seqs []int64
	var before int64
	for pages := 0; ; pages++ {
		if pages > 2 {
			t.Fatal("six rows in pages of three did not end after two pages")
		}
		page, err := db.PageMessages(a, pageQuery{List: messaging.ListInbox, Before: before, Limit: 3})
		if err != nil {
			t.Fatal(err)
		}
		for _, r := range page.Rows {
			if r.Content != "" {
				t.Fatal("a page read a body")
			}
			seqs = append(seqs, r.Seq)
		}
		if page.Next == 0 {
			break
		}
		before = page.Next
	}
	if len(seqs) != 6 {
		t.Fatalf("pages answered %v rows, want 6", len(seqs))
	}
	for i := 1; i < len(seqs); i++ {
		if seqs[i] >= seqs[i-1] {
			t.Fatalf("pages are not newest first: %v", seqs)
		}
	}
}

// One peer's page spans both boxes in one order, and a note a participant
// writes to itself keeps both its rows.
func TestAPeerPageSpansBothBoxes(t *testing.T) {
	db := testDB(t)
	a, b := astral.GenerateIdentity(), astral.GenerateIdentity()
	outbox(t, db, a, b)
	inbox(t, db, b, a)
	outbox(t, db, a, astral.GenerateIdentity()) // another conversation

	page, err := db.PageMessages(a, pageQuery{Peer: b, Limit: 10})
	if err != nil {
		t.Fatal(err)
	}
	if len(page.Rows) != 2 || page.Rows[0].Box != messaging.BoxInbox || page.Rows[1].Box != messaging.BoxOutbox {
		t.Fatalf("peer page: %v rows, want the inbox row then the outbox row", len(page.Rows))
	}

	self := messaging.NewMessageID()
	if err := db.InsertOutbox(&messaging.StoredMessage{ID: self, Sender: a, Recipient: a, Content: "x"}); err != nil {
		t.Fatal(err)
	}
	if _, err := db.InsertInbox(&messaging.StoredMessage{ID: self, Sender: a, Recipient: a, Content: "x"}); err != nil {
		t.Fatal(err)
	}
	page, err = db.PageMessages(a, pageQuery{Peer: a, Limit: 10})
	if err != nil || len(page.Rows) != 2 {
		t.Fatalf("a note to self: %v rows, err %v, want both rows", len(page.Rows), err)
	}
}

// A burst larger than a page drains in order, and a stamp on an old row, an
// archive and its undo all arrive as changes.
func TestChangesDrainEveryWriteInOrder(t *testing.T) {
	db := testDB(t)
	a, b := astral.GenerateIdentity(), astral.GenerateIdentity()
	old := outbox(t, db, a, b) // a send that answered nothing: created only

	first, err := db.MessageChanges(a, nil, 0, 100)
	if err != nil {
		t.Fatal(err)
	}
	since := first.Next

	for i := 0; i < 7; i++ {
		inbox(t, db, b, a)
	}
	if _, err := db.StampFetchedFrom(a, b, old); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Archive(a, messaging.BoxOutbox, old); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Unarchive(a, messaging.BoxOutbox, old); err != nil {
		t.Fatal(err)
	}

	var got []dbMessage
	for rounds := 0; ; rounds++ {
		if rounds > 3 {
			t.Fatal("changes did not drain")
		}
		page, err := db.MessageChanges(a, nil, since, 3)
		if err != nil {
			t.Fatal(err)
		}
		got = append(got, page.Rows...)
		since = page.Next
		if !page.More {
			break
		}
	}
	// seven arrivals and the old row once, at its latest state
	if len(got) != 8 {
		t.Fatalf("drained %v rows, want 8", len(got))
	}
	last := got[len(got)-1]
	if last.ID != old || last.FetchedAt == nil || last.ArchivedAt != nil {
		t.Fatal("the old row's last change is not its fetch, archive and undo")
	}

	again, err := db.MessageChanges(a, nil, since, 3)
	if err != nil || len(again.Rows) != 0 || again.Next != since {
		t.Fatalf("an empty read moved the cursor: next %v, want %v", again.Next, since)
	}
}

func TestTheSummaryFollowsTheConversation(t *testing.T) {
	db := testDB(t)
	a, b := astral.GenerateIdentity(), astral.GenerateIdentity()

	first := inbox(t, db, b, a)
	second := inbox(t, db, b, a)
	c := conversation(t, db, a, b)
	if c.Unread != 2 || c.Latest == nil || c.Latest.ID != second {
		t.Fatalf("after two arrivals: unread %v, want 2 and the second as latest", c.Unread)
	}

	if err := db.MarkRead(a, &messaging.StoredMessage{ID: first, Box: messaging.BoxInbox}); err != nil {
		t.Fatal(err)
	}
	if c = conversation(t, db, a, b); c.Unread != 1 {
		t.Fatalf("one read: unread %v, want 1", c.Unread)
	}

	// the newest put away: the latest falls back to the one before
	rev := c.Rev
	if _, err := db.Archive(a, messaging.BoxInbox, second); err != nil {
		t.Fatal(err)
	}
	c = conversation(t, db, a, b)
	if c.Latest == nil || c.Latest.ID != first || c.Unread != 0 || c.Rev <= rev {
		t.Fatal("archiving the newest did not fall back to the one before")
	}

	// the last put away: a tombstone, left out of pages and kept for changes
	if _, err := db.Archive(a, messaging.BoxInbox, first); err != nil {
		t.Fatal(err)
	}
	if c = conversation(t, db, a, b); c.LatestSeq != nil {
		t.Fatal("archiving the last did not leave a tombstone")
	}
	page, _ := db.PageConversations(a, nil, 0, 100)
	if len(page.Conversations) != 0 {
		t.Fatal("a tombstone stands in a page")
	}
	changes, _ := db.ConversationChanges(a, rev, 100)
	if n := len(changes.Conversations); n == 0 || changes.Conversations[n-1].LatestSeq != nil {
		t.Fatal("the tombstone is not a change")
	}

	if _, err := db.Unarchive(a, messaging.BoxInbox, first); err != nil {
		t.Fatal(err)
	}
	if c = conversation(t, db, a, b); c.Latest == nil || c.Latest.ID != first {
		t.Fatal("undo did not restore the conversation")
	}
}

// A fate on the latest row moves the summary; one on an older row does not.
func TestOnlyTheLatestRowsFateMovesTheSummary(t *testing.T) {
	db := testDB(t)
	a, b := astral.GenerateIdentity(), astral.GenerateIdentity()
	old := outbox(t, db, a, b)
	latest := outbox(t, db, a, b)

	rev := conversation(t, db, a, b).Rev
	if err := db.StampLanded(a, old); err != nil {
		t.Fatal(err)
	}
	if got := conversation(t, db, a, b).Rev; got != rev {
		t.Fatalf("a stamp on an older row moved the summary %v to %v", rev, got)
	}
	if err := db.StampLanded(a, latest); err != nil {
		t.Fatal(err)
	}
	c := conversation(t, db, a, b)
	if c.Rev <= rev || c.Latest.LandedAt == nil {
		t.Fatal("a stamp on the latest row did not move the summary")
	}
}

// One correspondent that writes a great deal leaves every other conversation a
// page away, and their unread marks with them.
func TestANoisyPeerStarvesNoConversation(t *testing.T) {
	db := testDB(t)
	a, noisy, quiet := astral.GenerateIdentity(), astral.GenerateIdentity(), astral.GenerateIdentity()
	inbox(t, db, quiet, a)
	for i := 0; i < 50; i++ {
		inbox(t, db, noisy, a)
	}

	page, err := db.PageConversations(a, nil, 0, 2)
	if err != nil {
		t.Fatal(err)
	}
	if len(page.Conversations) != 2 || page.Next != 0 {
		t.Fatalf("two conversations: %v rows, next %v", len(page.Conversations), page.Next)
	}
	if !page.Conversations[1].Peer.IsEqual(quiet) || page.Conversations[1].Unread != 1 {
		t.Fatal("the quiet conversation or its unread mark is missing")
	}
}

func TestDeletingAMailboxStartsANewGeneration(t *testing.T) {
	db := testDB(t)
	a, b := astral.GenerateIdentity(), astral.GenerateIdentity()
	if err := db.CreateMailbox(a, &astral.ObjectID{}, timeFar()); err != nil {
		t.Fatal(err)
	}
	inbox(t, db, b, a)

	before, _ := db.PageMessages(a, pageQuery{Limit: 10})
	if err := db.DeleteMailbox(a); err != nil {
		t.Fatal(err)
	}
	after, _ := db.PageConversations(a, nil, 0, 10)
	if after.Generation == before.Generation || len(after.Conversations) != 0 {
		t.Fatalf("generation %v → %v, %v conversations left", before.Generation, after.Generation, len(after.Conversations))
	}
}

// Every statement a page or a change read runs seeks an index: none scans a
// table or sorts in a temp b-tree, so the work is bounded by the page and not
// by the mailbox. The statements are the store's own, recorded as they ran.
func TestNoPagingReadScansOrSorts(t *testing.T) {
	db := testDB(t)
	a, b := astral.GenerateIdentity(), astral.GenerateIdentity()
	inbox(t, db, b, a)
	archived := inbox(t, db, b, a)
	if _, err := db.Archive(a, messaging.BoxInbox, archived); err != nil {
		t.Fatal(err)
	}

	for name, call := range map[string]func(*DB){
		"inbox page":         func(db *DB) { _, _ = db.PageMessages(a, pageQuery{List: messaging.ListInbox, Before: 9, Limit: 2}) },
		"outbox page":        func(db *DB) { _, _ = db.PageMessages(a, pageQuery{List: messaging.ListOutbox, Limit: 2}) },
		"archive page":       func(db *DB) { _, _ = db.PageMessages(a, pageQuery{List: messaging.ListArchive, Before: 9, Limit: 2}) },
		"peer page":          func(db *DB) { _, _ = db.PageMessages(a, pageQuery{Peer: b, Before: 9, Limit: 2}) },
		"changes":            func(db *DB) { _, _ = db.MessageChanges(a, nil, 1, 2) },
		"peer changes":       func(db *DB) { _, _ = db.MessageChanges(a, b, 1, 2) },
		"conversation page":  func(db *DB) { _, _ = db.PageConversations(a, nil, 9, 2) },
		"one conversation":   func(db *DB) { _, _ = db.PageConversations(a, b, 0, 2) },
		"conversation diffs": func(db *DB) { _, _ = db.ConversationChanges(a, 1, 2) },
	} {
		for _, sql := range recordSQL(t, db, call) {
			if !strings.HasPrefix(strings.TrimSpace(sql), "SELECT") {
				continue
			}
			plan := planOf(t, db, sql)
			if strings.Contains(plan, "SCAN messaging__") || strings.Contains(plan, "TEMP B-TREE") {
				t.Fatalf("%v: %v\nplans %v", name, sql, plan)
			}
		}
	}
}

// A store from before revisions takes them on its first start, each unique,
// and a restart resets nothing.
func TestAnOldStoreTakesRevisionsOnceAndKeepsThem(t *testing.T) {
	gdb, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	pool, _ := gdb.DB()
	pool.SetMaxOpenConns(1)
	db := &DB{DB: gdb}

	// the schema as it stood before this change
	if err := execAll(db.DB, ddlMailboxes); err != nil {
		t.Fatal(err)
	}
	if err := execAll(db.DB, append([]string{ddlMessages}, ddlIndexes...)); err != nil {
		t.Fatal(err)
	}
	a, b, c := astral.GenerateIdentity(), astral.GenerateIdentity(), astral.GenerateIdentity()
	insert := func(box string, from, to *astral.Identity) {
		err := db.Exec(`INSERT INTO messaging__messages (box, id, sender, recipient, content, created_at) VALUES (?, ?, ?, ?, 'x', '2026-01-01')`,
			box, messaging.NewMessageID(), from, to).Error
		if err != nil {
			t.Fatal(err)
		}
	}
	insert(messaging.BoxInbox, b, a)
	insert(messaging.BoxInbox, b, a)
	insert(messaging.BoxOutbox, a, c)

	if err := db.Migrate(); err != nil {
		t.Fatalf("first migration: %v", err)
	}
	var revs []int64
	db.Raw(`SELECT rev FROM messaging__messages UNION ALL SELECT rev FROM messaging__conversations`).Scan(&revs)
	seen := map[int64]bool{}
	for _, r := range revs {
		if r == 0 || seen[r] {
			t.Fatalf("backfilled revisions %v are not unique and nonzero", revs)
		}
		seen[r] = true
	}
	if got := conversation(t, db, a, b); got.Unread != 2 {
		t.Fatalf("backfilled unread %v, want 2", got.Unread)
	}

	var counter int64
	db.Raw(`SELECT rev FROM messaging__revisions`).Scan(&counter)
	if err := db.Migrate(); err != nil {
		t.Fatalf("restart: %v", err)
	}
	var again int64
	db.Raw(`SELECT rev FROM messaging__revisions`).Scan(&again)
	if again != counter {
		t.Fatalf("a restart moved the allocator %v to %v", counter, again)
	}

	inbox(t, db, b, a)
	if got := conversation(t, db, a, b); got.Unread != 3 {
		t.Fatalf("after migration an arrival left unread %v, want 3", got.Unread)
	}
}

// timeFar is an expiry no test outlives.
func timeFar() time.Time { return time.Now().Add(24 * time.Hour) }
