package messaging

import (
	"errors"
	"io"
	"strings"
	"testing"

	"github.com/astralp2p/astral-go/api/auth"
	"github.com/astralp2p/astral-go/api/messaging"
	"github.com/astralp2p/astral-go/astral"
	"github.com/astralp2p/astral-go/lib/query"
	authmod "github.com/astralp2p/astrald/mod/auth"
	"github.com/astralp2p/astrald/mod/nodes/frames"
)

// directionsAuth answers the two questions apart, which fakeAuth cannot: the
// sender's own side admits the target and the target's side turns the sender
// away. That pair is a participant whose inbound direction is off. Hosting is
// the real auth module's to answer, as for fakeAuth.
type directionsAuth struct {
	authmod.Module
	outbound, inbound bool
}

func (a *directionsAuth) Authorize(ctx *astral.Context, action auth.ActionObject) bool {
	switch action.(type) {
	case *messaging.SendAction:
		return a.outbound
	case *messaging.ReceiveAction:
		return a.inbound
	case *messaging.HostMailboxAction:
		return a.Module.Authorize(ctx, action)
	}
	return false
}

// linkedNode stands in for a recipient on another node. The far side gets what
// the link mux hands it: the query afresh, stamped with network origin and
// carrying none of this side's Extra. It maps the far side's routing error onto
// a reject code and back the way the link mux does, so a code that survives
// here survives a hop.
type linkedNode struct {
	identity *astral.Identity
	far      astral.Router
}

func (n *linkedNode) Identity() *astral.Identity { return n.identity }

func (n *linkedNode) RouteQuery(ctx *astral.Context, q *astral.InFlightQuery, w io.WriteCloser) (io.WriteCloser, error) {
	arrived := astral.Launch(&astral.Query{
		Nonce:       q.Nonce,
		Caller:      q.Caller,
		Target:      q.Target,
		QueryString: q.QueryString,
	})
	arrived.Extra.Set("origin", astral.OriginNetwork)

	rw, err := n.far.RouteQuery(ctx, arrived, w)
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

// refusingModule is a node whose participants' own side takes nothing from
// anybody.
func refusingModule(t *testing.T) *Module {
	t.Helper()
	mod := testMessagingModule(t)
	mod.Auth = &directionsAuth{Module: authorityOf(mod), outbound: true, inbound: false}
	return mod
}

// sendToPeer puts one root message to a peer, hosted on this node or not, and
// answers what the sending participant is told.
func sendToPeer(t *testing.T, mod *Module, hosted bool) error {
	t.Helper()

	sender, peer := hostedParticipant(t, mod), astral.GenerateIdentity()
	if hosted {
		peer = hostedParticipant(t, mod)
	}
	mod.Dir.(*stubDir).aliases["peer"] = peer

	var none messaging.MessageID
	_, err := mod.sendMessage(sender, "peer", "hello", none)
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
func TestARefusingParticipantAndAnAbsentOneReadApart(t *testing.T) {
	absent := testMessagingModule(t)
	absent.Auth = &directionsAuth{Module: authorityOf(absent), outbound: true, inbound: true}

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
	near.node = &linkedNode{identity: near.node.Identity(), far: far}

	// the peer's mailbox is hosted on the far node, which is what makes this a
	// hop: the near node hosts no such target and routes the query onward.
	sender, peer := hostedParticipant(t, near), hostedParticipant(t, far)
	near.Dir.(*stubDir).aliases["peer"] = peer

	var none messaging.MessageID
	_, err := near.sendMessage(sender, "peer", "hello", none)
	if err == nil {
		t.Fatal("a send the recipient never took must fail")
	}

	if local := sendToPeer(t, refusingModule(t), true).Error(); err.Error() != local {
		t.Fatalf("across a link the sender reads %q, on this node %q", err, local)
	}
}

// The words outlive the call: a participant that reads its outbox later finds
// the refusal on the row, not just a stamp saying the delivery failed.
func TestARefusalIsKeptOnTheOutboxRow(t *testing.T) {
	mod := refusingModule(t)

	sender, peer := hostedParticipant(t, mod), hostedParticipant(t, mod)
	mod.Dir.(*stubDir).aliases["peer"] = peer

	var none messaging.MessageID
	if _, err := mod.sendMessage(sender, "peer", "hello", none); err == nil {
		t.Fatal("a send the recipient never took must fail")
	}

	rows, err := mod.db.ListMessages(sender, messageQuery{List: messaging.ListOutbox})
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
