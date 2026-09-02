package mcp

import (
	"context"
	"testing"

	"github.com/astralp2p/astral-go/api/mcp"
	"github.com/astralp2p/astral-go/astral"
)

// Reading never stamps a row the reader does not own — one of the Done clauses.
func TestReadingStampsOnlyTheReadersOwnRow(t *testing.T) {
	mod := testMessageModule(t)
	a, b := astral.GenerateIdentity(), astral.GenerateIdentity()
	id := mcp.NewMessageID()

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

	ask, answer := mcp.NewMessageID(), mcp.NewMessageID()

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

// A reply that was put away must not come back as a child, body and all, with a
// receipt telling its sender it was collected. That would be the one path that
// undoes an archive.
func TestAnArchivedReplyIsNotAnsweredAsAChild(t *testing.T) {
	mod := testMessageModule(t)
	a, b := astral.GenerateIdentity(), astral.GenerateIdentity()
	ask, reply := mcp.NewMessageID(), mcp.NewMessageID()

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
	ask, reply := mcp.NewMessageID(), mcp.NewMessageID()

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
	held := mcp.NewMessageID()
	mustInsertInbox(t, mod, &dbMessage{ID: held, Sender: b, Recipient: a, Content: "here"})

	out, _, err := mod.db.ReadMany(a, []messageRef{
		{Box: boxInbox, ID: held},
		{Box: boxInbox, ID: mcp.NewMessageID()},
	})
	if err != nil {
		t.Fatalf("a batch with one unheld id must not fail: %v", err)
	}
	if len(out) != 1 || out[0].ID != held {
		t.Fatalf("the held message was not read: %+v", out)
	}
}

// ── the create path survives a node that was not reset ─────────────────────

// An agent that writes to itself owns both rows of every message, so a reply to
// it matches as a child twice under one id — once as the copy it sent and once
// as the copy it received. It is one reply and is answered once.
func TestASelfSentReplyIsAnsweredOnce(t *testing.T) {
	mod := testMessageModule(t)
	a := astral.GenerateIdentity()
	ask, reply := mcp.NewMessageID(), mcp.NewMessageID()

	for _, m := range []struct {
		id     mcp.MessageID
		parent mcp.MessageID
	}{{ask, mcp.MessageID{}}, {reply, ask}} {
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
	ask, reply := mcp.NewMessageID(), mcp.NewMessageID()

	mustInsertOutbox(t, mod, &dbMessage{ID: ask, Sender: a, Recipient: a, Content: "q"})
	mustInsertOutbox(t, mod, &dbMessage{ID: reply, Sender: a, Recipient: a, Content: "r", ParentID: ask})

	kids, err := mod.db.Children(a, ask, 10)
	if err != nil || len(kids) != 1 {
		t.Fatalf("the only copy of the reply was hidden: %v rows, err %v", len(kids), err)
	}
}

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
		id := mcp.NewMessageID()
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
	ask, reply := mcp.NewMessageID(), mcp.NewMessageID()

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
		over[i] = messageRef{Box: boxInbox, ID: mcp.NewMessageID()}
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
	ask := mcp.NewMessageID()
	mustInsertOutbox(t, mod, &dbMessage{ID: ask, Sender: a, Recipient: b, Content: "q"})

	const replies = maxChildren + 7
	want := make(map[string]bool, replies)
	for range replies {
		id := mcp.NewMessageID()
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
	ask, reply := mcp.NewMessageID(), mcp.NewMessageID()
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
	id := mcp.NewMessageID()
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
