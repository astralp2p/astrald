package mcp

import (
	"context"
	"testing"

	"github.com/astralp2p/astral-go/api/messaging"
	"github.com/astralp2p/astral-go/astral"
)

// A send is the authenticated agent's, and the parent it names reaches
// messaging as the id it parses to.
func TestASendIsTheAgentsOwn(t *testing.T) {
	mod, msg := testAgentModule(t)
	agent := astral.GenerateIdentity()
	parent := messaging.NewMessageID()
	msg.sent = messaging.NewMessageID()

	_, out, err := mod.sendMessageTool(agent)(context.Background(), nil, sendMessageIn{
		To: "peer", Content: "hello", ParentID: parent.String(),
	})
	if err != nil {
		t.Fatalf("send: %v", err)
	}

	if out.ID != msg.sent.String() {
		t.Fatalf("answered id %v, want messaging's %v", out.ID, msg.sent)
	}
	if msg.sendReq.To != "peer" || msg.sendReq.Content != "hello" || msg.sendReq.ParentID != parent {
		t.Fatalf("the send reached messaging as %+v", msg.sendReq)
	}

	msg.checkCalledAs(t, agent)
}
