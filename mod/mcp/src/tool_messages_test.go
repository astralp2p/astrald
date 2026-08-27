package mcp

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/astralp2p/astral-go/astral"
)

// The inbox lists what a message is and who sent it, and never its body: an
// agent chooses what to read before reading it.
func TestInboxToolListsWithoutBodies(t *testing.T) {
	mod := testMessageModule(t)
	sender, recipient := astral.GenerateIdentity(), astral.GenerateIdentity()
	_ = mod.Dir.SetAlias(sender, "scout")

	storeOne(t, mod, sender, recipient, testID(1), "one")
	storeOne(t, mod, sender, recipient, testID(2), "two")

	_, out, err := mod.inboxTool(recipient)(context.Background(), nil, inboxIn{})
	if err != nil {
		t.Fatalf("inbox: %v", err)
	}
	if len(out.Messages) != 2 {
		t.Fatalf("%v listed, want 2", len(out.Messages))
	}

	first := out.Messages[0]
	if first.ID != testID(1).String() || first.Topic != "one" {
		t.Fatalf("first entry %+v", first)
	}
	if first.Sender != sender.String() || first.SenderAlias != "scout" {
		t.Fatalf("sender %v/%v", first.Sender, first.SenderAlias)
	}
	if first.Read {
		t.Fatal("a message nobody read reads as read")
	}
}

func TestInboxToolFiltersUnread(t *testing.T) {
	mod := testMessageModule(t)
	sender, recipient := astral.GenerateIdentity(), astral.GenerateIdentity()

	storeOne(t, mod, sender, recipient, testID(1), "one")
	storeOne(t, mod, sender, recipient, testID(2), "two")

	if _, _, err := mod.readMessageTool(recipient)(context.Background(), nil, readMessageIn{ID: testID(1).String()}); err != nil {
		t.Fatalf("read: %v", err)
	}

	_, out, err := mod.inboxTool(recipient)(context.Background(), nil, inboxIn{UnreadOnly: true})
	if err != nil {
		t.Fatalf("inbox: %v", err)
	}
	if len(out.Messages) != 1 || out.Messages[0].ID != testID(2).String() {
		t.Fatalf("unread %+v, want b alone", out.Messages)
	}
}

func TestReadMessageToolReturnsBodyAndMarksRead(t *testing.T) {
	mod := testMessageModule(t)
	sender, recipient := astral.GenerateIdentity(), astral.GenerateIdentity()

	storeOne(t, mod, sender, recipient, testID(1), "one")

	_, out, err := mod.readMessageTool(recipient)(context.Background(), nil, readMessageIn{ID: testID(1).String()})
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if out.Content != "body of one" || out.Sender != sender.String() {
		t.Fatalf("read %+v", out)
	}

	_, listed, err := mod.inboxTool(recipient)(context.Background(), nil, inboxIn{})
	if err != nil {
		t.Fatalf("inbox: %v", err)
	}
	if !listed.Messages[0].Read {
		t.Fatal("the message read does not list as read")
	}
}

func TestReadMessageToolRefusesAnotherInbox(t *testing.T) {
	mod := testMessageModule(t)

	storeOne(t, mod, astral.GenerateIdentity(), astral.GenerateIdentity(), testID(1), "one")

	_, _, err := mod.readMessageTool(astral.GenerateIdentity())(
		context.Background(), nil, readMessageIn{ID: testID(1).String()})
	if err != errNoSuchMessage {
		t.Fatalf("read: got %v, want %v", err, errNoSuchMessage)
	}
}

func TestReadNextToolClaimsOldest(t *testing.T) {
	mod := testMessageModule(t)
	sender, recipient := astral.GenerateIdentity(), astral.GenerateIdentity()

	storeOne(t, mod, sender, recipient, testID(1), "one")
	storeOne(t, mod, sender, recipient, testID(2), "two")

	_, out, err := mod.readNextTool(recipient)(context.Background(), nil, readNextIn{TimeoutMs: 500})
	if err != nil {
		t.Fatalf("read_next: %v", err)
	}
	if out.Status != "message" || out.ID != testID(1).String() || out.Content != "body of one" {
		t.Fatalf("read_next %+v", out)
	}
}

func TestReadNextToolTimesOut(t *testing.T) {
	mod := testMessageModule(t)

	_, out, err := mod.readNextTool(astral.GenerateIdentity())(
		context.Background(), nil, readNextIn{TimeoutMs: 50})
	if err != nil {
		t.Fatalf("read_next: %v", err)
	}
	if out.Status != "timeout" || out.ID != "" {
		t.Fatalf("read_next %+v, want a bare timeout", out)
	}
}

// The read window is capped by config: a caller asking for longer than the
// node allows waits the node's window, not its own.
func TestReadNextToolCapsTheWindow(t *testing.T) {
	mod := testMessageModule(t)
	mod.config.ReadTimeout = 60 * time.Millisecond

	start := time.Now()
	if _, _, err := mod.readNextTool(astral.GenerateIdentity())(
		context.Background(), nil, readNextIn{TimeoutMs: 60000}); err != nil {
		t.Fatalf("read_next: %v", err)
	}

	if waited := time.Since(start); waited > time.Second {
		t.Fatalf("waited %v past a %v window", waited, mod.config.ReadTimeout)
	}
}

// A recipient the agent may not reach reads as one that does not resolve: the
// agent learns it cannot reach this one, and not whether this one exists.
func TestSendMessageToolRefusesUnauthorized(t *testing.T) {
	mod := testMessageModule(t)
	mod.Auth = &fakeAuth{allow: false}

	recipient := astral.GenerateIdentity()
	_ = mod.Dir.SetAlias(recipient, "beta")

	_, _, err := mod.sendMessageTool(astral.GenerateIdentity())(
		context.Background(), nil, sendMessageIn{To: "beta", Content: "hello"})
	if err == nil || !strings.Contains(err.Error(), "unknown recipient") {
		t.Fatalf("send: got %v, want an unknown recipient", err)
	}
}

func TestSendMessageToolRefusesUnknownRecipient(t *testing.T) {
	mod := testMessageModule(t)

	_, _, err := mod.sendMessageTool(astral.GenerateIdentity())(
		context.Background(), nil, sendMessageIn{To: "nobody", Content: "hello"})
	if err == nil || !strings.Contains(err.Error(), "unknown recipient") {
		t.Fatalf("send: got %v, want an unknown recipient", err)
	}
}

func TestSendMessageToolRefusesOversizeContent(t *testing.T) {
	mod := testMessageModule(t)

	recipient := astral.GenerateIdentity()
	_ = mod.Dir.SetAlias(recipient, "beta")

	_, _, err := mod.sendMessageTool(astral.GenerateIdentity())(
		context.Background(), nil, sendMessageIn{
			To:      "beta",
			Content: strings.Repeat("x", mod.config.MaxPayloadBytes+1),
		})
	if err == nil || !strings.Contains(err.Error(), "over") {
		t.Fatalf("send: got %v, want a size refusal", err)
	}
}

// An identifier the model made up is the model's own mistake, and the tool
// says so rather than reporting an inbox that holds no such message.
func TestReadMessageToolRefusesMalformedID(t *testing.T) {
	mod := testMessageModule(t)

	_, _, err := mod.readMessageTool(astral.GenerateIdentity())(
		context.Background(), nil, readMessageIn{ID: "not-an-id"})
	if err == nil || !strings.Contains(err.Error(), "message id") {
		t.Fatalf("read: got %v, want an invalid id", err)
	}
}

func TestSendMessageToolRefusesMalformedReplyTo(t *testing.T) {
	mod := testMessageModule(t)

	recipient := astral.GenerateIdentity()
	_ = mod.Dir.SetAlias(recipient, "beta")

	_, _, err := mod.sendMessageTool(astral.GenerateIdentity())(
		context.Background(), nil, sendMessageIn{
			To:      "beta",
			Content: "hello",
			ReplyTo: "not-an-id",
		})
	if err == nil || !strings.Contains(err.Error(), "message id") {
		t.Fatalf("send: got %v, want an invalid id", err)
	}
}
