package mcp

import (
	"context"
	"testing"

	"github.com/astralp2p/astral-go/api/messaging"
	"github.com/astralp2p/astral-go/astral"
)

// A message carries the ids of every direct reply, however many there are, and
// the tool renders every one of them — the replies the answer carries are
// bounded, and the ids are how a reader sees past that bound.
func TestAReadNamesEveryReplyEvenPastWhatItCarries(t *testing.T) {
	mod, msg := testAgentModule(t)
	a, b := astral.GenerateIdentity(), astral.GenerateIdentity()
	ask := testEnvelope(a, b, 1)
	ask.Box = messaging.BoxOutbox

	const replies = messaging.MaxChildren + 7
	ids := make([]messaging.MessageID, replies)
	for i := range ids {
		ids[i] = messaging.NewMessageID()
	}
	body := astral.String32("q")
	msg.read = &messaging.ReadMessagesResult{
		Messages: []*messaging.ReadMessage{{Envelope: ask, Content: &body, ChildIDs: ids}},
	}

	_, out, err := mod.readMessagesTool(a)(context.Background(), nil, readMessagesIn{
		IDs: []messageRefIn{{Box: messaging.BoxOutbox, ID: ask.ID.String()}},
	})
	if err != nil {
		t.Fatalf("read: %v", err)
	}

	if len(out.Messages[0].ChildIDs) != replies {
		t.Fatalf("named %v of %v replies", len(out.Messages[0].ChildIDs), replies)
	}
	for i, id := range out.Messages[0].ChildIDs {
		if id != ids[i].String() {
			t.Fatalf("child_ids[%v] is %v, want %v", i, id, ids[i])
		}
	}

	msg.checkCalledAs(t, a)
}

// A body the read withheld renders as no content, and truncated says why.
// max_children past the wire's width reaches messaging clamped, not wrapped.
func TestAWithheldBodyIsAbsentAndSaysSo(t *testing.T) {
	mod, msg := testAgentModule(t)
	a := astral.GenerateIdentity()
	env := testEnvelope(astral.GenerateIdentity(), a, 1)
	msg.read = &messaging.ReadMessagesResult{
		Messages: []*messaging.ReadMessage{{Envelope: env, Truncated: true}},
	}

	_, out, err := mod.readMessagesTool(a)(context.Background(), nil, readMessagesIn{
		IDs:         []messageRefIn{{Box: messaging.BoxInbox, ID: env.ID.String()}},
		Children:    messaging.ChildrenNone,
		MaxChildren: 1<<16 + 1,
	})
	if err != nil {
		t.Fatalf("read: %v", err)
	}

	if out.Messages[0].Content != "" || !out.Messages[0].Truncated {
		t.Fatalf("a withheld body rendered as %+v", out.Messages[0])
	}
	if msg.readReq.Children != messaging.ChildrenNone {
		t.Fatalf("children reached messaging as %q", msg.readReq.Children)
	}
	if msg.readReq.MaxChildren != messaging.MaxChildren {
		t.Fatalf("max_children reached messaging as %v, want %v", msg.readReq.MaxChildren, messaging.MaxChildren)
	}
}

// The replies a read answers come back flat beside the messages, each naming
// the message it answers, and a ref the read did not find comes back under
// not_found as the box and id the agent named.
func TestAReadRendersRepliesAndWhatItDidNotFind(t *testing.T) {
	mod, msg := testAgentModule(t)
	a := astral.GenerateIdentity()
	ask := testEnvelope(astral.GenerateIdentity(), a, 1)
	reply := testEnvelope(a, ask.Sender, 2)
	reply.Box, reply.ParentID = messaging.BoxOutbox, ask.ID
	missing := messaging.NewMessageID()

	body := astral.String32("q")
	msg.read = &messaging.ReadMessagesResult{
		Messages: []*messaging.ReadMessage{{Envelope: ask, Content: &body, ChildIDs: []messaging.MessageID{reply.ID}}},
		Replies:  []*messaging.ReadMessage{{Envelope: reply}},
		NotFound: []*messaging.MessageRef{{Box: messaging.BoxOutbox, ID: missing}},
	}

	_, out, err := mod.readMessagesTool(a)(context.Background(), nil, readMessagesIn{
		IDs: []messageRefIn{{Box: messaging.BoxInbox, ID: ask.ID.String()}, {Box: messaging.BoxOutbox, ID: missing.String()}},
	})
	if err != nil {
		t.Fatalf("read: %v", err)
	}

	if len(msg.readReq.Refs) != 2 || msg.readReq.Refs[1].ID != missing {
		t.Fatalf("the read reached messaging naming %v refs, want both the agent named", len(msg.readReq.Refs))
	}
	if len(out.Replies) != 1 || out.Replies[0].ID != reply.ID.String() || out.Replies[0].ParentID != ask.ID.String() {
		t.Fatalf("replies rendered as %+v, want the one reply naming its parent", out.Replies)
	}
	if out.Replies[0].Content != "" {
		t.Fatal("a reply answered as an envelope rendered a body")
	}
	want := messageRefIn{Box: messaging.BoxOutbox, ID: missing.String()}
	if len(out.NotFound) != 1 || out.NotFound[0] != want {
		t.Fatalf("not_found rendered as %+v, want %+v", out.NotFound, want)
	}

	msg.checkCalledAs(t, a)
}
