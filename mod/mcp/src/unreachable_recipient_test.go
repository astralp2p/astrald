package mcp

import (
	"errors"
	"io"
	"strings"
	"testing"

	"github.com/astralp2p/astral-go/api/auth"
	"github.com/astralp2p/astral-go/api/mcp"
	"github.com/astralp2p/astral-go/astral"
	"github.com/astralp2p/astral-go/lib/query"
	authmod "github.com/astralp2p/astrald/mod/auth"
	"github.com/astralp2p/astrald/mod/nodes/frames"
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

// linkedNode stands in for a recipient on another node. It maps the far side's
// routing error onto a reject code and back the way the link mux does, so a
// code that survives here survives a hop.
type linkedNode struct {
	identity *astral.Identity
	far      astral.Router
}

func (n *linkedNode) Identity() *astral.Identity { return n.identity }

func (n *linkedNode) RouteQuery(ctx *astral.Context, q *astral.InFlightQuery, w io.WriteCloser) (io.WriteCloser, error) {
	rw, err := n.far.RouteQuery(ctx, q, w)
	if err == nil {
		return rw, nil
	}

	code := uint8(frames.CodeRejected)
	var rejected *astral.ErrRejected
	if errors.As(err, &rejected) {
		code = rejected.Code
	}
	return query.RejectWithCode(code)
}

// refusingModule is an agent registered on this node whose own side takes
// nothing from anybody.
func refusingModule(t *testing.T) *Module {
	t.Helper()
	mod := testMessageModule(t)
	mod.Auth = &directionsAuth{outbound: true, inbound: false}
	return mod
}

// sendToPeer puts one root message to a peer, registered on this node or not,
// and answers what the sending agent is told.
func sendToPeer(t *testing.T, mod *Module, registered bool) error {
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
	return err
}

// A sender turned away is told so, and told it in the mailbox's own words: the
// answer it must act on is that this recipient takes nothing from it, so it
// stops rather than retrying a route that was never missing.
func TestASenderTurnedAwayIsToldSo(t *testing.T) {
	read := sendToPeer(t, refusingModule(t), true).Error()

	if !strings.Contains(read, errNotAdmitted.Error()) {
		t.Fatalf("the sender reads %q, want it named a refusal", read)
	}
	for _, leak := range []string{"route not found", "query rejected", "did not leave this node"} {
		if strings.Contains(read, leak) {
			t.Fatalf("the sender reads routing vocabulary %q: %v", leak, read)
		}
	}
}

// An identity nobody's node holds still reads as an absence, and the two words
// are not the same: a refusal is permanent and an absence may not be.
func TestARefusingAgentAndAnAbsentOneReadApart(t *testing.T) {
	absent := testMessageModule(t)
	absent.Auth = &directionsAuth{outbound: true, inbound: true}

	refused := sendToPeer(t, refusingModule(t), true).Error()
	missing := sendToPeer(t, absent, false).Error()

	if refused == missing {
		t.Fatalf("a refusal and an absence both read %q", refused)
	}
	if !strings.Contains(missing, errUnreachable.Error()) {
		t.Fatalf("an absent recipient reads %q, want an absence", missing)
	}
}

// The reject code is what carries the refusal, so it must survive the mapping a
// link puts it through. A recipient on another node reads the same as one here.
func TestARefusalSurvivesAHop(t *testing.T) {
	near, far := refusingModule(t), refusingModule(t)
	near.node = &linkedNode{identity: astral.GenerateIdentity(), far: far}

	// the agent is registered on the far node, which is what makes this a hop:
	// the near node holds no such target and routes the query onward.
	agent, peer := astral.GenerateIdentity(), astral.GenerateIdentity()
	near.Dir.(*stubDir).aliases["peer"] = peer
	_ = far.agentIDs.Add(peer.String())

	var none mcp.MessageID
	_, err := near.sendMessage(agent, "peer", "hello", none)
	if err == nil {
		t.Fatal("a send the recipient never took must fail")
	}

	if local := sendToPeer(t, refusingModule(t), true).Error(); err.Error() != local {
		t.Fatalf("across a link the sender reads %q, on this node %q", err, local)
	}
}

// The words outlive the call: an agent that reads its outbox later finds the
// refusal on the row, not just a stamp saying the delivery failed.
func TestARefusalIsKeptOnTheOutboxRow(t *testing.T) {
	mod := refusingModule(t)

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
	if rows[0].Err == nil || !strings.Contains(string(*rows[0].Err), errNotAdmitted.Error()) {
		t.Fatalf("the row keeps %v, want the refusal", rows[0].Err)
	}
}
