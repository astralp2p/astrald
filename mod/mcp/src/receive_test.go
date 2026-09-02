package mcp

import (
	"bytes"
	"testing"

	"github.com/astralp2p/astral-go/api/mcp"
	"github.com/astralp2p/astral-go/astral"
)

// A sender that retries after a lost acknowledgement sends the same id twice,
// and the recipient must hold one message rather than two.
func TestRouteQueryStoresMessage(t *testing.T) {
	mod := testMessageModule(t)
	recipient := astral.GenerateIdentity()
	_ = mod.agentIDs.Add(recipient.String())

	obj := deliverOverRouter(t, mod, recipient, &mcp.Message{
		ID:      testID(1),
		Content: astral.String32("the index is rebuilt"),
	})

	if _, ok := obj.(*astral.Ack); !ok {
		t.Fatalf("delivery answered %T, want an ack", obj)
	}

	rows, _, err := mod.db.ReadMany(recipient, []messageRef{{Box: mcp.BoxInbox, ID: testID(1)}})
	if err != nil {
		t.Fatalf("read stored: %v", err)
	}
	if len(rows) != 1 || rows[0].Content != "the index is rebuilt" {
		t.Fatalf("stored %+v", rows)
	}
}

func TestRouteQueryRefusesOversizeMessage(t *testing.T) {
	mod := testMessageModule(t)
	recipient := astral.GenerateIdentity()
	_ = mod.agentIDs.Add(recipient.String())

	obj := deliverOverRouter(t, mod, recipient, &mcp.Message{
		ID:      testID(1),
		Content: astral.String32(bytes.Repeat([]byte("x"), mod.config.MaxPayloadBytes+1)),
	})

	if _, ok := obj.(astral.Error); !ok {
		t.Fatalf("delivery answered %T, want an error", obj)
	}

	rows, err := mod.listMessages(recipient, listRequest{List: listInbox})
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(rows) != 0 {
		t.Fatalf("%v messages stored despite the refusal", len(rows))
	}
}
