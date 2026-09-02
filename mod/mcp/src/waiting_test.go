package mcp

import (
	"context"
	"testing"
	"time"

	"github.com/astralp2p/astral-go/api/mcp"
	"github.com/astralp2p/astral-go/astral"
)

// Nothing is claimed: two waiters are answered the same message and neither
// takes anything from the other.
func TestWaitTakesNothing(t *testing.T) {
	mod := testMessageModule(t)
	a, b := astral.GenerateIdentity(), astral.GenerateIdentity()
	mustInsertInbox(t, mod, &mcp.StoredMessage{ID: mcp.NewMessageID(), Sender: b, Recipient: a, Content: "x"})

	first, err := mod.waitMessages(context.Background(), a, waitRequest{Timeout: time.Millisecond})
	if err != nil || len(first.Rows) != 1 {
		t.Fatalf("first wait: %v rows, err %v", len(first.Rows), err)
	}
	second, err := mod.waitMessages(context.Background(), a, waitRequest{Timeout: time.Millisecond})
	if err != nil || len(second.Rows) != 1 {
		t.Fatalf("second wait: %v rows, err %v", len(second.Rows), err)
	}
	if first.Rows[0].ReadAt != nil || second.Rows[0].ReadAt != nil {
		t.Fatal("waiting stamped a row")
	}
}
