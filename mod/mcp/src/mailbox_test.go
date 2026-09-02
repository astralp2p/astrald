package mcp

import (
	"context"
	"sync"
	"testing"
	"time"

	mcpapi "github.com/astralp2p/astral-go/api/mcp"
	"github.com/astralp2p/astral-go/astral"
)

// mustInsertInbox stores a delivered message, failing the test if the write
// found a row already there.
func mustInsertInbox(t *testing.T, mod *Module, row *dbMessage) {
	t.Helper()
	n, err := mod.db.InsertInbox(row)
	if err != nil {
		t.Fatalf("insert inbox: %v", err)
	}
	if n != 1 {
		t.Fatalf("insert inbox: stored %v rows, want 1", n)
	}
}

func mustInsertOutbox(t *testing.T, mod *Module, row *dbMessage) {
	t.Helper()
	if err := mod.db.InsertOutbox(row); err != nil {
		t.Fatalf("insert outbox: %v", err)
	}
}

// ── the schema is what the DDL says, not what a struct tag implies ──────────

// The table carries a generated column, three CHECKs and a partial index, none
// of which a gorm tag can express. table_info omits a generated column
// entirely, so the shape has to be read through table_xinfo or `owner` is
// asserted by nothing at all.
func TestSchemaCarriesWhatTheDDLDeclares(t *testing.T) {
	mod := testMessageModule(t)

	var cols []struct {
		Name   string
		Hidden int
	}
	if err := mod.db.Raw(`SELECT name, hidden FROM pragma_table_xinfo('mcp__messages')`).Scan(&cols).Error; err != nil {
		t.Fatalf("table_xinfo: %v", err)
	}

	var owner bool
	for _, c := range cols {
		if c.Name == "owner" {
			owner = true
			if c.Hidden != 3 {
				t.Fatalf("owner hidden=%v, want 3 (stored generated)", c.Hidden)
			}
		}
	}
	if !owner {
		t.Fatal("the generated owner column is absent")
	}

	var idx []struct {
		Name    string
		Unique  int `gorm:"column:unique"`
		Partial int
	}
	if err := mod.db.Raw(`SELECT name, "unique", partial FROM pragma_index_list('mcp__messages')`).Scan(&idx).Error; err != nil {
		t.Fatalf("index_list: %v", err)
	}

	want := map[string][2]int{
		"ux_mcp__messages":         {1, 0},
		"ix_mcp__messages_box":     {0, 0},
		"ix_mcp__messages_parent":  {0, 0},
		"ix_mcp__messages_archive": {0, 1},
	}
	got := map[string][2]int{}
	for _, i := range idx {
		got[i.Name] = [2]int{i.Unique, i.Partial}
	}
	for name, w := range want {
		g, ok := got[name]
		if !ok {
			t.Fatalf("index %v is absent", name)
		}
		if g != w {
			t.Fatalf("index %v: unique/partial %v, want %v", name, g, w)
		}
	}
}

// owner is the database's to compute. Nothing writes it and no statement may
// change it, which is the guarantee a plain column cannot give.
func TestOwnerIsGeneratedAndUnwritable(t *testing.T) {
	mod := testMessageModule(t)
	a, b := astral.GenerateIdentity(), astral.GenerateIdentity()
	id := mcpapi.NewMessageID()

	mustInsertOutbox(t, mod, &dbMessage{ID: id, Sender: a, Recipient: b, Content: "x"})
	mustInsertInbox(t, mod, &dbMessage{ID: id, Sender: a, Recipient: b, Content: "x"})

	for _, c := range []struct {
		box   string
		owner *astral.Identity
	}{{boxOutbox, a}, {boxInbox, b}} {
		var row dbMessage
		if err := mod.db.Where("box = ? AND id = ?", c.box, id).Take(&row).Error; err != nil {
			t.Fatalf("%v: %v", c.box, err)
		}
		if !row.Owner.IsEqual(c.owner) {
			t.Fatalf("%v owner %v, want %v", c.box, row.Owner, c.owner)
		}
	}

	err := mod.db.Exec(`UPDATE mcp__messages SET owner = ? WHERE id = ?`, b, id).Error
	if err == nil {
		t.Fatal("owner must not be writable")
	}
}

// A column only one box may carry is refused on the other, so a statement that
// names the wrong box fails loudly rather than writing the wrong row.
func TestTheBoxChecksRefuseACrossBoxWrite(t *testing.T) {
	mod := testMessageModule(t)
	a, b := astral.GenerateIdentity(), astral.GenerateIdentity()
	id := mcpapi.NewMessageID()
	mustInsertInbox(t, mod, &dbMessage{ID: id, Sender: a, Recipient: b, Content: "x"})

	if err := mod.db.Exec(`UPDATE mcp__messages SET landed_at = ? WHERE id = ?`, "2026-01-01", id).Error; err == nil {
		t.Fatal("landed_at on an inbox row must be refused")
	}
	if err := mod.db.Exec(`INSERT INTO mcp__messages (box,id,sender,recipient,content,created_at) VALUES ('archive',?,?,?,'x','2026-01-01')`, mcpapi.NewMessageID(), a, b).Error; err == nil {
		t.Fatal("archive is a state, not a box, and must be refused")
	}
}

// ── one owner may hold two rows under one id ───────────────────────────────

// An agent writing to itself holds both rows, and a peer may mint an id this
// agent already sent under. The id is the peer's to choose, so neither is
// refusable — and both are why every statement names the box as well as the
// owner.
func TestOneOwnerHoldsTwoRowsUnderOneID(t *testing.T) {
	mod := testMessageModule(t)
	a, c := astral.GenerateIdentity(), astral.GenerateIdentity()
	id := mcpapi.NewMessageID()

	mustInsertOutbox(t, mod, &dbMessage{ID: id, Sender: a, Recipient: astral.GenerateIdentity(), Content: "sent"})
	mustInsertInbox(t, mod, &dbMessage{ID: id, Sender: c, Recipient: a, Content: "a peer minted the same id"})

	var n int64
	mod.db.Model(&dbMessage{}).Where("owner = ? AND id = ?", a, id).Count(&n)
	if n != 2 {
		t.Fatalf("owner+id matched %v rows, want 2", n)
	}

	rows, _, err := mod.db.ReadMany(a, []messageRef{{Box: boxInbox, ID: id}})
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if len(rows) != 1 || rows[0].Content != "a peer minted the same id" {
		t.Fatalf("the box did not pick the row: %+v", rows)
	}
}

// Reading never stamps a row the reader does not own — one of the Done clauses.
func TestReadingStampsOnlyTheReadersOwnRow(t *testing.T) {
	mod := testMessageModule(t)
	a, b := astral.GenerateIdentity(), astral.GenerateIdentity()
	id := mcpapi.NewMessageID()

	mustInsertOutbox(t, mod, &dbMessage{ID: id, Sender: a, Recipient: b, Content: "x"})
	mustInsertInbox(t, mod, &dbMessage{ID: id, Sender: a, Recipient: b, Content: "x"})

	if _, _, err := mod.db.ReadMany(b, []messageRef{{Box: boxInbox, ID: id}}); err != nil {
		t.Fatalf("read: %v", err)
	}

	var sent dbMessage
	if err := mod.db.Where("owner = ? AND box = ?", a, boxOutbox).Take(&sent).Error; err != nil {
		t.Fatalf("sender's row: %v", err)
	}
	if sent.ReadAt != nil {
		t.Fatal("the sender's own row was stamped read")
	}
	if sent.FetchedAt != nil {
		t.Fatal("the sender's row was stamped fetched by a read it did not make")
	}
}

// ── replies ────────────────────────────────────────────────────────────────

// Every reply names its parent — one of the Done clauses — and the replies to a
// message you sent are your own inbox rows, so one indexed lookup answers both
// directions.
func TestRepliesAreFoundInEitherBox(t *testing.T) {
	mod := testMessageModule(t)
	a, b := astral.GenerateIdentity(), astral.GenerateIdentity()

	ask, answer := mcpapi.NewMessageID(), mcpapi.NewMessageID()

	mustInsertOutbox(t, mod, &dbMessage{ID: ask, Sender: a, Recipient: b, Content: "which port?"})
	mustInsertInbox(t, mod, &dbMessage{ID: ask, Sender: a, Recipient: b, Content: "which port?"})
	mustInsertOutbox(t, mod, &dbMessage{ID: answer, Sender: b, Recipient: a, Content: "8626", ParentID: ask})
	mustInsertInbox(t, mod, &dbMessage{ID: answer, Sender: b, Recipient: a, Content: "8626", ParentID: ask})

	// A's reply arrived in A's inbox; B's own answer is in B's outbox.
	for _, c := range []struct {
		who *astral.Identity
		box string
	}{{a, boxInbox}, {b, boxOutbox}} {
		kids, err := mod.db.Children(c.who, ask, 10)
		if err != nil {
			t.Fatalf("children: %v", err)
		}
		if len(kids) != 1 || kids[0].ID != answer || kids[0].Box != c.box {
			t.Fatalf("children of the question: %+v, want one %v row", kids, c.box)
		}
	}
}

// A message may not answer itself. A parent this node does not hold is kept as
// it stands, because a claim about a message nobody has is a claim nothing
// answers — but a cycle of one is the cheapest to refuse.
func TestStoreRefusesASelfParentAndKeepsADanglingOne(t *testing.T) {
	mod := testMessageModule(t)
	a, b := astral.GenerateIdentity(), astral.GenerateIdentity()

	id := mcpapi.NewMessageID()
	err := mod.storeMessage(a, b, &mcpapi.Message{ID: id, Content: "x", ParentID: id})
	if err == nil {
		t.Fatal("a message answering itself must be refused")
	}

	stranger := mcpapi.NewMessageID()
	if err := mod.storeMessage(a, b, &mcpapi.Message{ID: mcpapi.NewMessageID(), Content: "x", ParentID: stranger}); err != nil {
		t.Fatalf("a parent this node does not hold must be kept: %v", err)
	}
}

// A second sender minting an id this inbox already holds is not the same as the
// first sender retrying, and answering both with an ack loses a message.
func TestStoreTellsARetryFromACollision(t *testing.T) {
	mod := testMessageModule(t)
	a, b, c := astral.GenerateIdentity(), astral.GenerateIdentity(), astral.GenerateIdentity()
	id := mcpapi.NewMessageID()

	if err := mod.storeMessage(a, b, &mcpapi.Message{ID: id, Content: "first"}); err != nil {
		t.Fatalf("first delivery: %v", err)
	}
	if err := mod.storeMessage(a, b, &mcpapi.Message{ID: id, Content: "first"}); err != nil {
		t.Fatalf("the same sender retrying must be accepted: %v", err)
	}
	if err := mod.storeMessage(c, b, &mcpapi.Message{ID: id, Content: "hijack"}); err == nil {
		t.Fatal("a different sender under a held id must be refused")
	}
}

// ── archive ────────────────────────────────────────────────────────────────

// An archived message never reappears from wait — one of the Done clauses — and
// leaves both listings while answering under types: archive.
func TestArchivedMessagesLeaveTheListingsAndTheWait(t *testing.T) {
	mod := testMessageModule(t)
	a, b := astral.GenerateIdentity(), astral.GenerateIdentity()
	id := mcpapi.NewMessageID()
	mustInsertInbox(t, mod, &dbMessage{ID: id, Sender: a, Recipient: b, Content: "x"})

	n, err := mod.db.Archive(b, boxInbox, id)
	if err != nil || n != 1 {
		t.Fatalf("archive: n=%v err=%v", n, err)
	}
	if n, _ := mod.db.Archive(b, boxInbox, id); n != 0 {
		t.Fatal("archiving twice must report the second call changed nothing")
	}

	live, err := mod.listMessages(b, listRequest{List: listInbox})
	if err != nil || len(live) != 0 {
		t.Fatalf("inbox after archiving: %v rows, err %v", len(live), err)
	}

	away, err := mod.listMessages(b, listRequest{List: listArchive})
	if err != nil || len(away) != 1 {
		t.Fatalf("archive: %v rows, err %v", len(away), err)
	}

	rows, err := mod.waitMessages(context.Background(), b, waitRequest{Timeout: time.Millisecond})
	if err != nil {
		t.Fatalf("wait: %v", err)
	}
	if len(rows) != 0 {
		t.Fatal("wait answered a message that was put away")
	}

	if n, _ := mod.db.Unarchive(b, boxInbox, id); n != 1 {
		t.Fatal("unarchive must put it back")
	}
	live, _ = mod.listMessages(b, listRequest{List: listInbox})
	if len(live) != 1 {
		t.Fatal("an unarchived message must return to the inbox")
	}
}

// Archiving is scoped like every other write: an id another agent holds is not
// this agent's to put away.
func TestArchiveIsScopedToItsOwner(t *testing.T) {
	mod := testMessageModule(t)
	a, b := astral.GenerateIdentity(), astral.GenerateIdentity()
	id := mcpapi.NewMessageID()
	mustInsertInbox(t, mod, &dbMessage{ID: id, Sender: a, Recipient: b, Content: "x"})

	if n, _ := mod.db.Archive(a, boxInbox, id); n != 0 {
		t.Fatal("an agent archived a message it does not own")
	}
}

// ── the three lists, and the filters that belong to each ───────────────────

func TestEachListAnswersItsOwnRowsInItsOwnOrder(t *testing.T) {
	mod := testMessageModule(t)
	a, b := astral.GenerateIdentity(), astral.GenerateIdentity()

	for i := range 3 {
		_ = i
		id := mcpapi.NewMessageID()
		mustInsertOutbox(t, mod, &dbMessage{ID: id, Sender: a, Recipient: b, Content: "sent"})
		mustInsertInbox(t, mod, &dbMessage{ID: mcpapi.NewMessageID(), Sender: b, Recipient: a, Content: "got"})
	}

	in, err := mod.listMessages(a, listRequest{List: listInbox})
	if err != nil || len(in) != 3 {
		t.Fatalf("inbox: %v rows, err %v", len(in), err)
	}
	for _, row := range in {
		if row.Box != boxInbox {
			t.Fatalf("an outbox row answered the inbox: %+v", row)
		}
	}
	if !in[0].CreatedAt.Before(in[2].CreatedAt) {
		t.Fatal("an inbox is a queue and reads oldest first")
	}

	out, err := mod.listMessages(a, listRequest{List: listOutbox})
	if err != nil || len(out) != 3 {
		t.Fatalf("outbox: %v rows, err %v", len(out), err)
	}
	if !out[0].CreatedAt.After(out[2].CreatedAt) {
		t.Fatal("a sent list is a history and reads newest first")
	}
}

// A filter that names a column null by construction in the list being read is
// refused, rather than silently answering everything or nothing.
func TestAFilterThatCannotApplyIsRefused(t *testing.T) {
	for _, c := range []struct {
		name string
		q    messageQuery
	}{
		{"awaiting_pickup on an inbox", messageQuery{List: listInbox, AwaitingPickup: true}},
		{"unread_only on an outbox", messageQuery{List: listOutbox, UnreadOnly: true}},
		{"to on an inbox", messageQuery{List: listInbox, To: astral.GenerateIdentity()}},
		{"from on an outbox", messageQuery{List: listOutbox, From: astral.GenerateIdentity()}},
		{"unread_only on the archive", messageQuery{List: listArchive, UnreadOnly: true}},
		{"a list that is not one", messageQuery{List: "elsewhere"}},
	} {
		t.Run(c.name, func(t *testing.T) {
			if err := c.q.validate(); err == nil {
				t.Fatal("must be refused")
			}
		})
	}
}

// A listing answers the whole list. An agent asking what is in its own mailbox
// and being handed a prefix has been told something silently false, and it has
// no way to see that from the answer.
func TestAListingAnswersTheWholeList(t *testing.T) {
	mod := testMessageModule(t)
	a, b := astral.GenerateIdentity(), astral.GenerateIdentity()

	const held = 500
	for range held {
		mustInsertInbox(t, mod, &dbMessage{ID: mcpapi.NewMessageID(), Sender: b, Recipient: a, Content: "x"})
	}

	rows, err := mod.listMessages(a, listRequest{List: listInbox})
	if err != nil || len(rows) != held {
		t.Fatalf("listed %v of %v (err %v)", len(rows), held, err)
	}
}

// ── wait ───────────────────────────────────────────────────────────────────

// Nothing is claimed: two waiters are answered the same message and neither
// takes anything from the other.
func TestWaitTakesNothing(t *testing.T) {
	mod := testMessageModule(t)
	a, b := astral.GenerateIdentity(), astral.GenerateIdentity()
	mustInsertInbox(t, mod, &dbMessage{ID: mcpapi.NewMessageID(), Sender: b, Recipient: a, Content: "x"})

	first, err := mod.waitMessages(context.Background(), a, waitRequest{Timeout: time.Millisecond})
	if err != nil || len(first) != 1 {
		t.Fatalf("first wait: %v rows, err %v", len(first), err)
	}
	second, err := mod.waitMessages(context.Background(), a, waitRequest{Timeout: time.Millisecond})
	if err != nil || len(second) != 1 {
		t.Fatalf("second wait: %v rows, err %v", len(second), err)
	}
	if first[0].ReadAt != nil || second[0].ReadAt != nil {
		t.Fatal("waiting stamped a row")
	}
}

// since narrows a set that is already durably defined, so a stale value costs a
// repeat and never a message.
func TestSinceOnlyNarrows(t *testing.T) {
	mod := testMessageModule(t)
	a, b := astral.GenerateIdentity(), astral.GenerateIdentity()
	mustInsertInbox(t, mod, &dbMessage{ID: mcpapi.NewMessageID(), Sender: b, Recipient: a, Content: "one"})

	rows, err := mod.listMessages(a, listRequest{List: listInbox})
	if err != nil || len(rows) != 1 {
		t.Fatalf("seed: %v rows, err %v", len(rows), err)
	}
	since := nextSince(rows)
	if since == "" {
		t.Fatal("a listing that answered a row must hand out a cursor")
	}

	after, err := mod.listMessages(a, listRequest{List: listInbox, Since: since})
	if err != nil || len(after) != 0 {
		t.Fatalf("since its own answer: %v rows, err %v", len(after), err)
	}

	mustInsertInbox(t, mod, &dbMessage{ID: mcpapi.NewMessageID(), Sender: b, Recipient: a, Content: "two"})
	after, err = mod.listMessages(a, listRequest{List: listInbox, Since: since})
	if err != nil || len(after) != 1 || after[0].Content != "two" {
		t.Fatalf("after a new arrival: %+v, err %v", after, err)
	}

	// An empty since is a superset, never a subset.
	all, _ := mod.listMessages(a, listRequest{List: listInbox})
	if len(all) != 2 {
		t.Fatalf("dropping since must widen: %v rows", len(all))
	}
}

// ── the cursor pages the database's order, not a clock ─────────────────────

// created_at is read in Go before the INSERT, so a row can carry an earlier
// instant and commit later. A cursor over it steps past a message that had not
// appeared when the cursor was handed out, and never answers it again. seq is
// assigned under the write lock, so paging it can only narrow.
//
// This is the test that fails if the cursor ever goes back to a timestamp.
func TestTheCursorNeverSkipsAMessage(t *testing.T) {
	mod := testMessageModule(t)
	a := astral.GenerateIdentity()

	const senders, each = 4, 25

	var wg sync.WaitGroup
	for range senders {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for range each {
				peer := astral.GenerateIdentity()
				_, _ = mod.db.InsertInbox(&dbMessage{
					ID: mcpapi.NewMessageID(), Sender: peer, Recipient: a, Content: "x",
				})
			}
		}()
	}

	// A reader paging with the cursor each answer hands it, running the whole
	// time the senders are writing.
	seen := map[string]bool{}
	var since string
	done := make(chan struct{})
	go func() {
		defer close(done)
		for {
			rows, err := mod.listMessages(a, listRequest{List: listInbox, Since: since})
			if err != nil {
				t.Error(err)
				return
			}
			for _, row := range rows {
				seen[row.ID.String()] = true
			}
			if next := nextSince(rows); next != "" {
				since = next
			}
			select {
			case <-done:
				return
			default:
			}
			if len(seen) == senders*each {
				return
			}
		}
	}()

	wg.Wait()
	<-done

	// A final page, the way a reader resuming would.
	rows, err := mod.listMessages(a, listRequest{List: listInbox, Since: since})
	if err != nil {
		t.Fatalf("final page: %v", err)
	}
	for _, row := range rows {
		seen[row.ID.String()] = true
	}

	var held int64
	mod.db.Model(&dbMessage{}).Where("owner = ? AND box = ?", a, boxInbox).Count(&held)

	if int64(len(seen)) != held {
		t.Fatalf("the cursor showed %v of %v messages — %v were stepped past",
			len(seen), held, held-int64(len(seen)))
	}
}

// A cursor names a position in an order. The other two lists are histories read
// newest first, so a cursor on them can only lose rows and is refused.
func TestSinceIsRefusedOnAHistory(t *testing.T) {
	for _, list := range []string{listOutbox, listArchive} {
		q := messageQuery{List: list, Since: 1}
		if err := q.validate(); err == nil {
			t.Fatalf("since on the %v must be refused", list)
		}
	}
}

// ── archive is a filter everywhere, or it is not one ───────────────────────

// A reply that was put away must not come back as a child, body and all, with a
// receipt telling its sender it was collected. That would be the one path that
// undoes an archive.
func TestAnArchivedReplyIsNotAnsweredAsAChild(t *testing.T) {
	mod := testMessageModule(t)
	a, b := astral.GenerateIdentity(), astral.GenerateIdentity()
	ask, reply := mcpapi.NewMessageID(), mcpapi.NewMessageID()

	mustInsertOutbox(t, mod, &dbMessage{ID: ask, Sender: a, Recipient: b, Content: "q"})
	mustInsertInbox(t, mod, &dbMessage{ID: reply, Sender: b, Recipient: a, Content: "SECRET", ParentID: ask})

	if n, _ := mod.db.Archive(a, boxInbox, reply); n != 1 {
		t.Fatal("archive the reply")
	}

	kids, err := mod.db.Children(a, ask, 10)
	if err != nil {
		t.Fatalf("children: %v", err)
	}
	if len(kids) != 0 {
		t.Fatalf("an archived reply was answered as a child: %+v", kids)
	}

	ids, err := mod.db.ChildIDs(a, ask)
	if err != nil || len(ids) != 0 {
		t.Fatalf("child_ids named %v archived replies, err %v — it must name the set that is answerable", len(ids), err)
	}
}

// A body handed out stamps the row it came from. Otherwise the sender reads it
// collected while the reader's own unread_only still lists it.
func TestAChildsBodyStampsTheRowItCameFrom(t *testing.T) {
	mod := testMessageModule(t)
	a, b := astral.GenerateIdentity(), astral.GenerateIdentity()
	ask, reply := mcpapi.NewMessageID(), mcpapi.NewMessageID()

	mustInsertOutbox(t, mod, &dbMessage{ID: ask, Sender: a, Recipient: b, Content: "q"})
	mustInsertInbox(t, mod, &dbMessage{ID: reply, Sender: b, Recipient: a, Content: "the answer", ParentID: ask})

	_, _, err := mod.readMessagesTool(a)(context.Background(), nil, readMessagesIn{
		IDs:      []messageRefIn{{Box: boxOutbox, ID: ask.String()}},
		Children: childrenFull,
	})
	if err != nil {
		t.Fatalf("read: %v", err)
	}

	var row dbMessage
	if err := mod.db.Where("owner = ? AND box = ? AND id = ?", a, boxInbox, reply).Take(&row).Error; err != nil {
		t.Fatalf("the reply: %v", err)
	}
	if row.ReadAt == nil {
		t.Fatal("a body was handed out without stamping the row it came from")
	}

	unread, err := mod.listMessages(a, listRequest{List: listInbox, UnreadOnly: true})
	if err != nil || len(unread) != 0 {
		t.Fatalf("unread_only still lists a message whose body was handed out: %v rows, err %v", len(unread), err)
	}
}

// An id the owner does not hold is reported; a store that cannot answer is an
// error, not a claim about the agent's own mailbox.
func TestAnUnheldIDIsReportedAndTheRestAreStillRead(t *testing.T) {
	mod := testMessageModule(t)
	a, b := astral.GenerateIdentity(), astral.GenerateIdentity()
	held := mcpapi.NewMessageID()
	mustInsertInbox(t, mod, &dbMessage{ID: held, Sender: b, Recipient: a, Content: "here"})

	out, _, err := mod.db.ReadMany(a, []messageRef{
		{Box: boxInbox, ID: held},
		{Box: boxInbox, ID: mcpapi.NewMessageID()},
	})
	if err != nil {
		t.Fatalf("a batch with one unheld id must not fail: %v", err)
	}
	if len(out) != 1 || out[0].ID != held {
		t.Fatalf("the held message was not read: %+v", out)
	}
}

// ── the create path survives a node that was not reset ─────────────────────

// A table already standing makes CREATE TABLE IF NOT EXISTS a no-op, and every
// statement after it fails against a shape with no box column — which loads no
// module, which serves the agents no tools. The old table is renamed aside so
// the create path runs.
func TestAnOldTableIsRenamedAsideRatherThanBreakingTheLoad(t *testing.T) {
	db := testDB(t)

	// stand up something shaped like the old inbox, under the live name
	if err := db.Exec(`DROP TABLE mcp__messages`).Error; err != nil {
		t.Fatalf("drop: %v", err)
	}
	if err := db.Exec(`CREATE TABLE mcp__messages (id text PRIMARY KEY, thread text, stored_at datetime)`).Error; err != nil {
		t.Fatalf("old table: %v", err)
	}
	if err := db.Exec(`INSERT INTO mcp__messages (id, thread) VALUES ('old','old')`).Error; err != nil {
		t.Fatalf("old row: %v", err)
	}

	if err := db.MigrateMessages(); err != nil {
		t.Fatalf("migrate over an old table: %v", err)
	}

	if !db.Migrator().HasColumn(&dbMessage{}, "box") {
		t.Fatal("the create path did not run")
	}
	if !db.Migrator().HasTable("mcp__messages_v1") {
		t.Fatal("the old table was not kept aside")
	}

	// and the new table works
	a, b := astral.GenerateIdentity(), astral.GenerateIdentity()
	n, err := db.InsertInbox(&dbMessage{ID: mcpapi.NewMessageID(), Sender: b, Recipient: a, Content: "x"})
	if err != nil || n != 1 {
		t.Fatalf("insert after the rename: n=%v err=%v", n, err)
	}
}

// An agent that writes to itself owns both rows of every message, so a reply to
// it matches as a child twice under one id — once as the copy it sent and once
// as the copy it received. It is one reply and is answered once.
func TestASelfSentReplyIsAnsweredOnce(t *testing.T) {
	mod := testMessageModule(t)
	a := astral.GenerateIdentity()
	ask, reply := mcpapi.NewMessageID(), mcpapi.NewMessageID()

	for _, m := range []struct {
		id     mcpapi.MessageID
		parent mcpapi.MessageID
	}{{ask, mcpapi.MessageID{}}, {reply, ask}} {
		mustInsertOutbox(t, mod, &dbMessage{ID: m.id, Sender: a, Recipient: a, Content: "x", ParentID: m.parent})
		mustInsertInbox(t, mod, &dbMessage{ID: m.id, Sender: a, Recipient: a, Content: "x", ParentID: m.parent})
	}

	kids, err := mod.db.Children(a, ask, 10)
	if err != nil {
		t.Fatalf("children: %v", err)
	}
	if len(kids) != 1 {
		t.Fatalf("one reply was answered %v times: %+v", len(kids), kids)
	}
	if kids[0].Box != boxInbox {
		t.Fatalf("the copy kept must be the received one, which carries the read stamp; got %v", kids[0].Box)
	}

	ids, err := mod.db.ChildIDs(a, ask)
	if err != nil || len(ids) != 1 {
		t.Fatalf("child_ids named %v, err %v — it must name the set Children answers", len(ids), err)
	}
}

// The collapse keeps the received copy only where there is one. A self-send
// whose delivery failed has an outbox row and no inbox row, and hiding it would
// lose the only copy the agent has.
func TestASelfSendThatNeverLandedIsStillAnswered(t *testing.T) {
	mod := testMessageModule(t)
	a := astral.GenerateIdentity()
	ask, reply := mcpapi.NewMessageID(), mcpapi.NewMessageID()

	mustInsertOutbox(t, mod, &dbMessage{ID: ask, Sender: a, Recipient: a, Content: "q"})
	mustInsertOutbox(t, mod, &dbMessage{ID: reply, Sender: a, Recipient: a, Content: "r", ParentID: ask})

	kids, err := mod.db.Children(a, ask, 10)
	if err != nil || len(kids) != 1 {
		t.Fatalf("the only copy of the reply was hidden: %v rows, err %v", len(kids), err)
	}
}

// A remote node's words land in this agent's sent list and from there in its
// model's context. They are quoted material, but how much of a context window a
// refusing peer occupies is this node's decision.
func TestARemoteRefusalIsBounded(t *testing.T) {
	long := ""
	for range 4000 {
		long += "x"
	}
	got := clip(long, maxRefusalBytes)
	if len(got) > maxRefusalBytes+len("… (cut)") {
		t.Fatalf("a refusing node wrote %v bytes into the agent's context", len(got))
	}
	if short := clip("refused", maxRefusalBytes); short != "refused" {
		t.Fatalf("text within the bound must be untouched, got %q", short)
	}
}

// undo runs through the same tool, so what the answer reports is whether this
// call moved the message — not whether it ended up archived, which under undo
// would name the opposite of what happened.
func TestArchiveReportsWhetherThisCallMovedIt(t *testing.T) {
	mod := testMessageModule(t)
	a, b := astral.GenerateIdentity(), astral.GenerateIdentity()
	id := mcpapi.NewMessageID()
	mustInsertInbox(t, mod, &dbMessage{ID: id, Sender: b, Recipient: a, Content: "x"})

	call := func(undo bool) bool {
		t.Helper()
		_, out, err := mod.archiveTool(a)(context.Background(), nil, archiveIn{
			Box: boxInbox, ID: id.String(), Undo: undo,
		})
		if err != nil {
			t.Fatalf("archive(undo=%v): %v", undo, err)
		}
		return out.Changed
	}

	if !call(false) {
		t.Fatal("the first archive must report that it moved the message")
	}
	if call(false) {
		t.Fatal("archiving what is already away must not report a move")
	}
	if !call(true) {
		t.Fatal("undo must report that it moved the message back")
	}
	if call(true) {
		t.Fatal("undoing what is already back must not report a move")
	}

	// an id the agent does not hold answers the same false, and says nothing
	// about whether it exists elsewhere
	_, out, err := mod.archiveTool(b)(context.Background(), nil, archiveIn{
		Box: boxInbox, ID: id.String(),
	})
	if err != nil || out.Changed {
		t.Fatalf("an agent moved a message it does not hold: changed=%v err=%v", out.Changed, err)
	}
}

// ── one answer has a size, and the caller does not choose it ───────────────

// A body may be 64 KiB and a read names up to twenty ids, so an unbounded
// answer is the senders deciding how much of the reader's context they fill.
// The bodies that do not fit are left out and say so; the messages themselves
// are still answered, because a read that returned fewer would look like a
// mailbox that lost them.
func TestAFullAnswerLeavesBodiesAndSaysSo(t *testing.T) {
	mod := testMessageModule(t)
	mod.config.MaxResponseBytes = 100

	a, b := astral.GenerateIdentity(), astral.GenerateIdentity()

	body := ""
	for range 60 {
		body += "x"
	}

	refs := make([]messageRef, 3)
	for i := range refs {
		id := mcpapi.NewMessageID()
		mustInsertInbox(t, mod, &dbMessage{ID: id, Sender: b, Recipient: a, Content: body})
		refs[i] = messageRef{Box: boxInbox, ID: id}
	}

	res, err := mod.readMessages(a, readRequest{Refs: refs, Children: childrenNone})
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if len(res.Messages) != 3 {
		t.Fatalf("a full answer must still name every message asked for, got %v", len(res.Messages))
	}

	if res.Messages[0].Truncated {
		t.Fatal("the first body fits and must be answered")
	}
	for _, m := range res.Messages[1:] {
		if !m.Truncated || !m.WithoutBody {
			t.Fatalf("a body past the budget must be left out and say so: %+v", m)
		}
	}
}

// The caller named the messages and did not name the replies, so an answer that
// runs out of room drops the extra rather than the thing that was asked for.
func TestRepliesAreChargedAfterTheMessageTheyAnswer(t *testing.T) {
	mod := testMessageModule(t)
	mod.config.MaxResponseBytes = 80

	a, b := astral.GenerateIdentity(), astral.GenerateIdentity()
	ask, reply := mcpapi.NewMessageID(), mcpapi.NewMessageID()

	body := ""
	for range 60 {
		body += "y"
	}
	mustInsertOutbox(t, mod, &dbMessage{ID: ask, Sender: a, Recipient: b, Content: body})
	mustInsertInbox(t, mod, &dbMessage{ID: reply, Sender: b, Recipient: a, Content: body, ParentID: ask})

	res, err := mod.readMessages(a, readRequest{
		Refs:     []messageRef{{Box: boxOutbox, ID: ask}},
		Children: childrenFull,
	})
	if err != nil {
		t.Fatalf("read: %v", err)
	}

	if res.Messages[0].Truncated {
		t.Fatal("the message the caller named must keep its body while there is room")
	}
	if len(res.Replies) != 1 || !res.Replies[0].Truncated {
		t.Fatalf("the reply is the extra and must be the one left out: %+v", res.Replies)
	}
}

// A read is bounded in how many ids it takes and how many replies it answers
// per message, and neither bound is the caller's to raise.
func TestAReadsBoundsAreTheModulesAndNotTheCallers(t *testing.T) {
	over := make([]messageRef, maxReadIDs+1)
	for i := range over {
		over[i] = messageRef{Box: boxInbox, ID: mcpapi.NewMessageID()}
	}
	if err := (&readRequest{Refs: over}).validate(); err == nil {
		t.Fatalf("naming more than %v ids in one read must be refused", maxReadIDs)
	}

	if err := (&readRequest{}).validate(); err == nil {
		t.Fatal("a read that names nothing must be refused")
	}

	req := readRequest{Refs: over[:1], MaxChildren: maxChildren + 500}
	if err := req.validate(); err != nil {
		t.Fatalf("validate: %v", err)
	}
	if req.MaxChildren != maxChildren {
		t.Fatalf("max_children was raised to %v past the module's %v", req.MaxChildren, maxChildren)
	}
	if req.Children != childrenEnvelopes {
		t.Fatalf("children defaults to envelopes, not %v", req.Children)
	}

	if err := (&readRequest{Refs: over[:1], Children: "everything"}).validate(); err == nil {
		t.Fatal("a children mode that is not one must be refused, not ignored")
	}
}

// ── the read answers the shape of the conversation ─────────────────────────

// A message carries the ids of every direct reply, however many there are. The
// replies the answer carries are bounded, so without the ids a reader could not
// see past that bound — and max_children cannot be raised past it either, which
// made the bound a wall rather than a page.
func TestAReadNamesEveryReplyEvenPastWhatItCarries(t *testing.T) {
	mod := testMessageModule(t)
	a, b := astral.GenerateIdentity(), astral.GenerateIdentity()
	ask := mcpapi.NewMessageID()
	mustInsertOutbox(t, mod, &dbMessage{ID: ask, Sender: a, Recipient: b, Content: "q"})

	const replies = maxChildren + 7
	want := make(map[string]bool, replies)
	for range replies {
		id := mcpapi.NewMessageID()
		mustInsertInbox(t, mod, &dbMessage{ID: id, Sender: b, Recipient: a, Content: "r", ParentID: ask})
		want[id.String()] = true
	}

	_, out, err := mod.readMessagesTool(a)(context.Background(), nil, readMessagesIn{
		IDs: []messageRefIn{{Box: boxOutbox, ID: ask.String()}},
	})
	if err != nil {
		t.Fatalf("read: %v", err)
	}

	if len(out.Messages[0].ChildIDs) != replies {
		t.Fatalf("named %v of %v replies", len(out.Messages[0].ChildIDs), replies)
	}
	for _, id := range out.Messages[0].ChildIDs {
		if !want[id] {
			t.Fatalf("child_ids named a message that is not a reply: %v", id)
		}
	}
	if len(out.Replies) != maxChildren {
		t.Fatalf("the answer carried %v replies, want the module's bound of %v", len(out.Replies), maxChildren)
	}

	// every id it named is one it will answer
	refs := make([]messageRefIn, 0, replies)
	for _, id := range out.Messages[0].ChildIDs {
		refs = append(refs, messageRefIn{Box: boxInbox, ID: id})
	}
	_, second, err := mod.readMessagesTool(a)(context.Background(), nil, readMessagesIn{
		IDs: refs, Children: childrenNone,
	})
	if err != nil {
		t.Fatalf("reading the ids it named: %v", err)
	}
	if len(second.NotFound) != 0 {
		t.Fatalf("child_ids named %v messages the read then refused", len(second.NotFound))
	}
}

// The ids are the shape of the conversation and the mode is how much of the
// replies' content the answer carries, so asking for no replies still says what
// there is to ask for next.
func TestChildIDsComeBackEvenWhenNoRepliesAreAskedFor(t *testing.T) {
	mod := testMessageModule(t)
	a, b := astral.GenerateIdentity(), astral.GenerateIdentity()
	ask, reply := mcpapi.NewMessageID(), mcpapi.NewMessageID()
	mustInsertOutbox(t, mod, &dbMessage{ID: ask, Sender: a, Recipient: b, Content: "q"})
	mustInsertInbox(t, mod, &dbMessage{ID: reply, Sender: b, Recipient: a, Content: "SECRET", ParentID: ask})

	_, out, err := mod.readMessagesTool(a)(context.Background(), nil, readMessagesIn{
		IDs:      []messageRefIn{{Box: boxOutbox, ID: ask.String()}},
		Children: childrenNone,
	})
	if err != nil {
		t.Fatalf("read: %v", err)
	}

	if len(out.Messages[0].ChildIDs) != 1 || out.Messages[0].ChildIDs[0] != reply.String() {
		t.Fatalf("children:none must still name the reply: %+v", out.Messages[0].ChildIDs)
	}
	if len(out.Replies) != 0 {
		t.Fatal("children:none must carry no replies")
	}

	// naming a reply is not handing it out: nothing was stamped and its sender
	// is not told it was collected
	var row dbMessage
	if err := mod.db.Where("owner = ? AND box = ? AND id = ?", a, boxInbox, reply).Take(&row).Error; err != nil {
		t.Fatalf("the reply: %v", err)
	}
	if row.ReadAt != nil {
		t.Fatal("naming a reply's id stamped it read")
	}
}

// Naming one message twice asks for it once. A read that answered it twice
// would charge the budget twice for a body the caller already has, and the
// bound on how many messages a read takes is a bound on distinct messages.
func TestNamingOneMessageTwiceReadsItOnce(t *testing.T) {
	mod := testMessageModule(t)
	a, b := astral.GenerateIdentity(), astral.GenerateIdentity()
	id := mcpapi.NewMessageID()
	mustInsertInbox(t, mod, &dbMessage{ID: id, Sender: b, Recipient: a, Content: "once"})

	ref := messageRef{Box: boxInbox, ID: id}
	res, err := mod.readMessages(a, readRequest{Refs: []messageRef{ref, ref, ref}, Children: childrenNone})
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if len(res.Messages) != 1 {
		t.Fatalf("one message named three times was answered %v times", len(res.Messages))
	}

	// the bound counts distinct messages, so a caller repeating itself is not
	// refused for a limit it never reached
	repeated := make([]messageRef, maxReadIDs+5)
	for i := range repeated {
		repeated[i] = ref
	}
	if err := (&readRequest{Refs: repeated}).validate(); err != nil {
		t.Fatalf("%v repeats of one id is one message, not %v: %v", len(repeated), len(repeated), err)
	}
}

// The box is half of what names a row, so an agent that holds both rows of one
// id named two messages rather than one twice.
func TestTheSameIDInBothBoxesIsTwoMessages(t *testing.T) {
	mod := testMessageModule(t)
	a := astral.GenerateIdentity()
	id := mcpapi.NewMessageID()
	mustInsertOutbox(t, mod, &dbMessage{ID: id, Sender: a, Recipient: a, Content: "sent"})
	mustInsertInbox(t, mod, &dbMessage{ID: id, Sender: a, Recipient: a, Content: "received"})

	res, err := mod.readMessages(a, readRequest{
		Refs:     []messageRef{{Box: boxOutbox, ID: id}, {Box: boxInbox, ID: id}},
		Children: childrenNone,
	})
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if len(res.Messages) != 2 {
		t.Fatalf("the two rows of a self-send are two messages, got %v", len(res.Messages))
	}
}
