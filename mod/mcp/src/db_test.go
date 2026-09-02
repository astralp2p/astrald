package mcp

import (
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/astralp2p/astral-go/api/mcp"
	"github.com/astralp2p/astral-go/astral"
)

// mustInsertInbox stores a delivered message, failing the test if the write
// found a row already there.
func mustInsertInbox(t *testing.T, mod *Module, m *mcp.StoredMessage) {
	t.Helper()
	n, err := mod.db.InsertInbox(m)
	if err != nil {
		t.Fatalf("insert inbox: %v", err)
	}
	if n != 1 {
		t.Fatalf("insert inbox: stored %v rows, want 1", n)
	}
}

func mustInsertOutbox(t *testing.T, mod *Module, m *mcp.StoredMessage) {
	t.Helper()
	if err := mod.db.InsertOutbox(m); err != nil {
		t.Fatalf("insert outbox: %v", err)
	}
}

// The schema is what the DDL says, not what a struct tag implies.

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
	id := mcp.NewMessageID()

	mustInsertOutbox(t, mod, &mcp.StoredMessage{ID: id, Sender: a, Recipient: b, Content: "x"})
	mustInsertInbox(t, mod, &mcp.StoredMessage{ID: id, Sender: a, Recipient: b, Content: "x"})

	for _, c := range []struct {
		box   string
		owner *astral.Identity
	}{{mcp.BoxOutbox, a}, {mcp.BoxInbox, b}} {
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
	id := mcp.NewMessageID()
	mustInsertInbox(t, mod, &mcp.StoredMessage{ID: id, Sender: a, Recipient: b, Content: "x"})

	if err := mod.db.Exec(`UPDATE mcp__messages SET landed_at = ? WHERE id = ?`, "2026-01-01", id).Error; err == nil {
		t.Fatal("landed_at on an inbox row must be refused")
	}
	if err := mod.db.Exec(`INSERT INTO mcp__messages (box,id,sender,recipient,content,created_at) VALUES ('archive',?,?,?,'x','2026-01-01')`, mcp.NewMessageID(), a, b).Error; err == nil {
		t.Fatal("archive is a state, not a box, and must be refused")
	}
}

// One owner may hold two rows under one id.

// An agent writing to itself holds both rows, and a peer may mint an id this
// agent already sent under. The id is the peer's to choose, so neither is
// refusable — and both are why every statement names the box as well as the
// owner.
func TestOneOwnerHoldsTwoRowsUnderOneID(t *testing.T) {
	mod := testMessageModule(t)
	a, c := astral.GenerateIdentity(), astral.GenerateIdentity()
	id := mcp.NewMessageID()

	mustInsertOutbox(t, mod, &mcp.StoredMessage{ID: id, Sender: a, Recipient: astral.GenerateIdentity(), Content: "sent"})
	mustInsertInbox(t, mod, &mcp.StoredMessage{ID: id, Sender: c, Recipient: a, Content: "a peer minted the same id"})

	var n int64
	mod.db.Model(&dbMessage{}).Where("owner = ? AND id = ?", a, id).Count(&n)
	if n != 2 {
		t.Fatalf("owner+id matched %v rows, want 2", n)
	}

	rows, _, err := mod.db.ReadMany(a, []messageRef{{Box: mcp.BoxInbox, ID: id}})
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if len(rows) != 1 || rows[0].Content != "a peer minted the same id" {
		t.Fatalf("the box did not pick the row: %+v", rows)
	}
}

// The box is half of what names a row, so an agent that holds both rows of one
// id named two messages rather than one twice.
func TestTheSameIDInBothBoxesIsTwoMessages(t *testing.T) {
	mod := testMessageModule(t)
	a := astral.GenerateIdentity()
	id := mcp.NewMessageID()
	mustInsertOutbox(t, mod, &mcp.StoredMessage{ID: id, Sender: a, Recipient: a, Content: "sent"})
	mustInsertInbox(t, mod, &mcp.StoredMessage{ID: id, Sender: a, Recipient: a, Content: "received"})

	res, err := mod.readMessages(a, readRequest{
		Refs:     []messageRef{{Box: mcp.BoxOutbox, ID: id}, {Box: mcp.BoxInbox, ID: id}},
		Children: childrenNone,
	})
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if len(res.Messages) != 2 {
		t.Fatalf("the two rows of a self-send are two messages, got %v", len(res.Messages))
	}
}

// A message may not answer itself. A parent this node does not hold is kept as
// it stands, because a claim about a message nobody has is a claim nothing
// answers — but a cycle of one is the cheapest to refuse.
func TestStoreRefusesASelfParentAndADanglingOne(t *testing.T) {
	mod := testMessageModule(t)
	a, b := astral.GenerateIdentity(), astral.GenerateIdentity()

	id := mcp.NewMessageID()
	err := mod.storeMessage(a, b, &mcp.Message{ID: id, Content: "x", ParentID: id})
	if err == nil {
		t.Fatal("a message answering itself must be refused")
	}

	// a parent this node does not hold is refused, and nothing is stored
	stranger := mcp.NewMessageID()
	child := mcp.NewMessageID()
	if err := mod.storeMessage(a, b, &mcp.Message{ID: child, Content: "x", ParentID: stranger}); err == nil {
		t.Fatal("a parent this node does not hold must be refused")
	}
	if held, _ := mod.db.Holds(b, child); held {
		t.Fatal("the refused message must not be stored")
	}
}

// A second sender minting an id this inbox already holds is not the same as the
// first sender retrying, and answering both with an ack loses a message.
func TestStoreTellsARetryFromACollision(t *testing.T) {
	mod := testMessageModule(t)
	a, b, c := astral.GenerateIdentity(), astral.GenerateIdentity(), astral.GenerateIdentity()
	id := mcp.NewMessageID()

	if err := mod.storeMessage(a, b, &mcp.Message{ID: id, Content: "first"}); err != nil {
		t.Fatalf("first delivery: %v", err)
	}
	if err := mod.storeMessage(a, b, &mcp.Message{ID: id, Content: "first"}); err != nil {
		t.Fatalf("the same sender retrying must be accepted: %v", err)
	}
	if err := mod.storeMessage(c, b, &mcp.Message{ID: id, Content: "hijack"}); err == nil {
		t.Fatal("a different sender under a held id must be refused")
	}
}

// Archive.

// A remote node's words land in this agent's sent list and from there in its
// model's context. They are quoted material, but how much of a context window a
// refusing peer occupies is this node's decision, taken where the row is
// written — the only place that bounds them.
func TestARemoteRefusalIsBoundedInTheStore(t *testing.T) {
	mod := testMessageModule(t)
	sender, recipient := astral.GenerateIdentity(), astral.GenerateIdentity()
	id := mcp.NewMessageID()

	mustInsertOutbox(t, mod, &mcp.StoredMessage{ID: id, Sender: sender, Recipient: recipient, Content: "x"})

	long := strings.Repeat("ł", 4000)
	if err := mod.db.SetErr(sender, id, long); err != nil {
		t.Fatal(err)
	}

	rows, err := mod.db.ListMessages(sender, messageQuery{List: listOutbox})
	if err != nil || len(rows) != 1 || rows[0].Err == nil {
		t.Fatalf("the refusal was not stored: %v rows, err %v", len(rows), err)
	}

	got := string(*rows[0].Err)
	if len(got) > errLimit+len("… (cut)") {
		t.Fatalf("a refusing node wrote %v bytes into the agent's context", len(got))
	}
	if !strings.HasSuffix(got, "… (cut)") {
		t.Fatalf("a cut refusal must say so, got %q", got[max(0, len(got)-16):])
	}
	if !utf8.ValidString(got) {
		t.Fatal("the cut split a rune")
	}

	if err = mod.db.SetErr(sender, mcp.NewMessageID(), "refused"); err != nil {
		t.Fatal(err)
	}
}
