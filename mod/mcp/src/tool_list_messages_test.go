package mcp

import (
	"bytes"
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/astralp2p/astral-go/api/messaging"
	"github.com/astralp2p/astral-go/astral"
)

// A listing with a cursor and nothing newer keeps the cursor, and one with
// something newer answers the furthest row.
func TestAListingKeepsTheCursor(t *testing.T) {
	mod, msg := testAgentModule(t)
	agent := astral.GenerateIdentity()
	list := mod.listMessagesTool(agent)

	msg.listed = []*messaging.Envelope{testEnvelope(astral.GenerateIdentity(), agent, 7)}
	_, first, err := list(context.Background(), nil, listMessagesIn{})
	if err != nil || first.NextSince != "7" {
		t.Fatalf("first listing: next_since %q, err %v", first.NextSince, err)
	}

	msg.listed = nil
	_, again, err := list(context.Background(), nil, listMessagesIn{Since: first.NextSince})
	if err != nil {
		t.Fatal(err)
	}
	if msg.listReq.Since != 7 {
		t.Fatalf("the cursor reached messaging as %v, want 7", msg.listReq.Since)
	}
	if again.NextSince != first.NextSince {
		t.Fatalf("cursor: got %q, want the one sent, %q", again.NextSince, first.NextSince)
	}

	msg.checkCalledAs(t, agent)
}

// A cursor of zero is a cursor sent, and it comes back as sent, from a listing
// and from a wait alike. Only a caller that sent no cursor is handed none while
// nothing carried one.
func TestACursorOfZeroComesBack(t *testing.T) {
	mod, msg := testAgentModule(t)
	agent := astral.GenerateIdentity()

	for since, want := range map[string]string{"": "", "0": "0"} {
		_, listed, err := mod.listMessagesTool(agent)(context.Background(), nil, listMessagesIn{Since: since})
		if err != nil {
			t.Fatalf("list with since %q: %v", since, err)
		}
		if listed.NextSince != want {
			t.Fatalf("an empty listing with since %q answered next_since %q, want %q", since, listed.NextSince, want)
		}

		msg.waited = &messaging.WaitResult{TimedOut: true}
		_, waited, err := mod.waitTool(agent)(context.Background(), nil, waitIn{Since: since, TimeoutSecs: 1})
		if err != nil {
			t.Fatalf("wait with since %q: %v", since, err)
		}
		if waited.NextSince != want {
			t.Fatalf("a timed-out wait with since %q answered next_since %q, want %q", since, waited.NextSince, want)
		}
	}
}

// A cursor the agent invented is refused before messaging is asked.
func TestAListingRefusesAnInventedCursor(t *testing.T) {
	mod, msg := testAgentModule(t)

	for _, since := range []string{"-1", "later", "18446744073709551615"} {
		_, _, err := mod.listMessagesTool(astral.GenerateIdentity())(context.Background(), nil, listMessagesIn{Since: since})
		if err == nil {
			t.Fatalf("since %q must be refused", since)
		}
	}
	if len(msg.callers) != 0 {
		t.Fatal("messaging was asked with a cursor no answer handed out")
	}
}

// A listing and a read answer the two parties by identity and nothing else. A
// display name resolves to this node's own alias for a key or, failing that, to
// a truncated form of the key itself — so the field could not be absent, and an
// agent had no way to tell a name it was given from one it was not.
func TestNoAnswerCarriesADisplayName(t *testing.T) {
	mod, msg := testAgentModule(t)
	a, b := astral.GenerateIdentity(), astral.GenerateIdentity()
	env := testEnvelope(b, a, 1)
	content := astral.String32("x")

	msg.listed = []*messaging.Envelope{env}
	msg.read = &messaging.ReadMessagesResult{
		Messages: []*messaging.ReadMessage{{Envelope: env, Content: &content}},
	}

	_, listed, err := mod.listMessagesTool(a)(context.Background(), nil, listMessagesIn{})
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	_, read, err := mod.readMessagesTool(a)(context.Background(), nil, readMessagesIn{
		IDs: []messageRefIn{{Box: messaging.BoxInbox, ID: env.ID.String()}},
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

	msg.checkCalledAs(t, a)
}

// testEnvelope is an inbox row from sender to owner at the given cursor.
func testEnvelope(sender, owner *astral.Identity, cursor uint64) *messaging.Envelope {
	return &messaging.Envelope{
		Cursor:    astral.Uint64(cursor),
		ID:        messaging.NewMessageID(),
		Box:       messaging.BoxInbox,
		Sender:    sender,
		Recipient: owner,
		CreatedAt: astral.Time(time.Now().UTC()),
	}
}
