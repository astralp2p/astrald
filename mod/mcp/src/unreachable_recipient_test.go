package mcp

import (
	"strings"
	"testing"

	"github.com/astralp2p/astral-go/api/auth"
	"github.com/astralp2p/astral-go/api/mcp"
	"github.com/astralp2p/astral-go/astral"
	authmod "github.com/astralp2p/astrald/mod/auth"
)

// directionsAuth answers the two questions apart, which fakeAuth cannot: the
// sender's own side admits the target and the target's side turns the sender
// away. That pair is an agent whose inbound direction is off.
type directionsAuth struct {
	authmod.Module
	outbound, inbound bool
}

func (a *directionsAuth) Authorize(_ *astral.Context, action auth.ActionObject) bool {
	switch action.(type) {
	case *mcp.CallAgentAction:
		return a.outbound
	case *mcp.AnswerAgentAction:
		return a.inbound
	}
	return false
}

// sendToPeer puts one root message to a peer, registered on this node or not,
// and answers what the sending agent is told.
func sendToPeer(t *testing.T, mod *Module, registered bool) string {
	t.Helper()

	agent, peer := astral.GenerateIdentity(), astral.GenerateIdentity()
	mod.Dir.(*stubDir).aliases["peer"] = peer
	if registered {
		_ = mod.agentIDs.Add(peer.String())
	}

	var none mcp.MessageID
	_, err := mod.sendMessage(agent, "peer", "hello", none)
	if err == nil {
		t.Fatal("a send the recipient never took must fail")
	}
	return err.Error()
}

// The sender is told what it can act on. The recipient's node answers one
// silence for three different causes, so the words name all three and the
// router's own vocabulary stays out of a mailbox's answer.
func TestASendNobodyTookNamesWhatTheSenderCanKnow(t *testing.T) {
	mod := testMessageModule(t)
	mod.Auth = &directionsAuth{outbound: true, inbound: false}

	read := sendToPeer(t, mod, true)

	if !strings.Contains(read, errUnreachable.Error()) {
		t.Fatalf("the sender reads %q, want it to name the three causes", read)
	}
	for _, leak := range []string{"route not found", "query rejected", "did not leave this node"} {
		if strings.Contains(read, leak) {
			t.Fatalf("the sender reads routing vocabulary %q: %v", leak, read)
		}
	}
}

// An agent whose inbound is off and an identity that is nobody's agent read the
// same, which is the collapse RouteQuery is built on: a sender learns that it
// cannot reach this recipient, and not whether the recipient exists.
func TestARefusingAgentAndAnAbsentOneReadTheSame(t *testing.T) {
	refusing := testMessageModule(t)
	refusing.Auth = &directionsAuth{outbound: true, inbound: false}

	absent := testMessageModule(t)
	absent.Auth = &directionsAuth{outbound: true, inbound: true}

	if a, b := sendToPeer(t, refusing, true), sendToPeer(t, absent, false); a != b {
		t.Fatalf("a refusing recipient reads %q and an absent one %q", a, b)
	}
}

// The words changed and the row did not: a send nobody took is still stamped
// failed, so the outbox still tells a delivered message from one that was not.
func TestASendNobodyTookIsStillStampedFailed(t *testing.T) {
	mod := testMessageModule(t)
	mod.Auth = &directionsAuth{outbound: true, inbound: false}

	agent, peer := astral.GenerateIdentity(), astral.GenerateIdentity()
	mod.Dir.(*stubDir).aliases["peer"] = peer
	_ = mod.agentIDs.Add(peer.String())

	var none mcp.MessageID
	if _, err := mod.sendMessage(agent, "peer", "hello", none); err == nil {
		t.Fatal("a send the recipient never took must fail")
	}

	rows, err := mod.db.ListMessages(agent, messageQuery{List: listOutbox})
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 1 {
		t.Fatalf("the outbox holds %v rows, want 1", len(rows))
	}
	if rows[0].FailedAt == nil {
		t.Fatal("a send nobody took must be stamped failed")
	}
}
