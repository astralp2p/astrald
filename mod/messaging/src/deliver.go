package messaging

import (
	"errors"
	"fmt"
	"time"

	"github.com/astralp2p/astral-go/api/messaging"
	"github.com/astralp2p/astral-go/astral"
	"github.com/astralp2p/astral-go/astral/channel"
	"github.com/astralp2p/astral-go/lib/query"
)

// deliverMessage puts the message to the recipient and returns once the
// recipient's node has stored it.
//
// why a query and not a write to the table: routing is what tells a recipient
// on another node from one here, and the local case loops back through
// RouteQuery onto the same path.
//
// why the context is the node's and does not name the sender: a link carries
// a query whose caller is not the context's identity as a relay query, naming
// the caller and the target. A context naming the sender makes the link carry
// a plain query, which the far node reads as this node querying itself.
func (mod *Module) deliverMessage(senderID, targetID *astral.Identity, msg *messaging.Message) error {
	qctx, cancel := mod.ctx.WithTimeout(mod.config.DeliveryTimeout)
	defer cancel()

	conn, err := query.RouteInFlight(qctx, mod.node, launchDelivery(senderID, targetID, messaging.MethodMessage))
	if err != nil {
		var rejected *astral.ErrRejected
		if errors.As(err, &rejected) && rejected.Code == messaging.RejectNotAdmitted {
			return errNotAdmitted
		}

		// why the router's own words are logged and not returned: they name a
		// routing outcome, and a participant reads a mailbox — send.go.
		mod.log.Logv(2, "outbox %v: routing to %v: %v", msg.ID, targetID, err)
		return errUnreachable
	}
	defer conn.Close()

	// why the deadline: the recipient's node answers, but the link it answers
	// over can stall. Closing the conn is what unblocks the read.
	timer := time.AfterFunc(mod.config.DeliveryTimeout, func() { conn.Close() })
	defer timer.Stop()

	ch := channel.New(conn)

	if err = ch.Send(msg); err != nil {
		return fmt.Errorf("%w: %v", errNotSent, err)
	}

	// why a lost answer is not a failure: the recipient's node may have stored
	// the message and died before acknowledging it.
	obj, err := ch.Receive()
	if err != nil {
		return fmt.Errorf("%w: %v", errNoAnswer, err)
	}

	if e, ok := obj.(astral.Error); ok {
		return fmt.Errorf("%w: %s", errRefused, e.Error())
	}
	if _, ok := obj.(*astral.Ack); !ok {
		return fmt.Errorf("%w: answered %v", errNoAnswer, obj.ObjectType())
	}

	return nil
}

// extraSendPath is the Extra key launchDelivery marks its queries with.
const extraSendPath = "messaging.send_path"

// sendPathMark is the value under extraSendPath. It is unexported, so no other
// package can set it: mod/mcp's launch and an apphost guest set only their own
// keys, and a value of another type under this key is not the mark.
type sendPathMark struct{}

// launchDelivery wraps a delivery or a receipt for routing, with no origin, and
// marks it as this module's own.
//
// why no origin is stamped: the path is fixed and addressed to a participant,
// never to a node, and this module serves no operation named after either path.
// A delivery aimed at the node identity therefore finds no operation behind it —
// see TestNoOperationAnswersADeliveryPath.
//
// why the mark: a local delivery or receipt is taken only from this module's
// own send path — see fromSendPath.
func launchDelivery(callerID, targetID *astral.Identity, path string) *astral.InFlightQuery {
	inFlight := astral.Launch(query.New(callerID, targetID, path, nil))
	inFlight.Extra.Set(extraSendPath, sendPathMark{})
	return inFlight
}

// fromSendPath answers whether a delivery or a receipt came from a send path:
// over a link, from the far node's, or from launchDelivery on this node.
//
// why only a send path's: the recipient's side asks receive_action alone and
// relies on the sending side having asked send_action, and a receipt on the
// reader's node having handed the body out. A query an agent's declared tool
// put, or a local app routed itself, did neither, and would store mail
// send_action refuses with no outbox row behind it.
func fromSendPath(q *astral.InFlightQuery) bool {
	if q.IsNetwork() {
		return true
	}

	v, ok := q.Extra.Get(extraSendPath)
	if !ok {
		return false
	}
	_, mark := v.(sendPathMark)
	return mark
}
