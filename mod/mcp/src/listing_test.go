package mcp

import (
	"bytes"
	"context"
	"encoding/json"
	"sync"
	"testing"

	"github.com/astralp2p/astral-go/api/mcp"
	"github.com/astralp2p/astral-go/astral"
)

func TestEachListAnswersItsOwnRowsInItsOwnOrder(t *testing.T) {
	mod := testMessageModule(t)
	a, b := astral.GenerateIdentity(), astral.GenerateIdentity()

	for i := range 3 {
		_ = i
		id := mcp.NewMessageID()
		mustInsertOutbox(t, mod, &dbMessage{ID: id, Sender: a, Recipient: b, Content: "sent"})
		mustInsertInbox(t, mod, &dbMessage{ID: mcp.NewMessageID(), Sender: b, Recipient: a, Content: "got"})
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
		mustInsertInbox(t, mod, &dbMessage{ID: mcp.NewMessageID(), Sender: b, Recipient: a, Content: "x"})
	}

	rows, err := mod.listMessages(a, listRequest{List: listInbox})
	if err != nil || len(rows) != held {
		t.Fatalf("listed %v of %v (err %v)", len(rows), held, err)
	}
}

// Wait.

// since narrows a set that is already durably defined, so a stale value costs a
// repeat and never a message.
func TestSinceOnlyNarrows(t *testing.T) {
	mod := testMessageModule(t)
	a, b := astral.GenerateIdentity(), astral.GenerateIdentity()
	mustInsertInbox(t, mod, &dbMessage{ID: mcp.NewMessageID(), Sender: b, Recipient: a, Content: "one"})

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

	mustInsertInbox(t, mod, &dbMessage{ID: mcp.NewMessageID(), Sender: b, Recipient: a, Content: "two"})
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

// The cursor pages the database's order, not a clock.

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
					ID: mcp.NewMessageID(), Sender: peer, Recipient: a, Content: "x",
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

// Archive is a filter everywhere, or it is not one.

// A listing and a read answer the two parties by identity and nothing else. A
// display name resolves to this node's own alias for a key or, failing that, to
// a truncated form of the key itself — so the field could not be absent, and an
// agent had no way to tell a name it was given from one it was not.
func TestNoAnswerCarriesADisplayName(t *testing.T) {
	mod := testMessageModule(t)
	a, b := astral.GenerateIdentity(), astral.GenerateIdentity()
	id := mcp.NewMessageID()
	mustInsertInbox(t, mod, &dbMessage{ID: id, Sender: b, Recipient: a, Content: "x"})

	_, listed, err := mod.listMessagesTool(a)(context.Background(), nil, listMessagesIn{})
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	_, read, err := mod.readMessagesTool(a)(context.Background(), nil, readMessagesIn{
		IDs: []messageRefIn{{Box: boxInbox, ID: id.String()}},
	})
	if err != nil {
		t.Fatalf("read: %v", err)
	}

	for label, v := range map[string]any{"list_messages": listed, "read_messages": read} {
		blob, err := json.Marshal(v)
		if err != nil {
			t.Fatalf("%v: %v", label, err)
		}
		if bytes.Contains(blob, []byte("peer_alias")) || bytes.Contains(blob, []byte("alias")) {
			t.Fatalf("%v still answers a display name: %s", label, blob)
		}
	}

	// the parties are still named, by the value that identifies them
	if listed.Messages[0].Peer != b.String() {
		t.Fatalf("the listing must still name the peer: %+v", listed.Messages[0])
	}
	if read.Messages[0].Sender != b.String() || read.Messages[0].Recipient != a.String() {
		t.Fatalf("the read must still name both parties: %+v", read.Messages[0])
	}
}
