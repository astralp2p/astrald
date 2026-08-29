package mcp

import (
	"context"
	"errors"
	"fmt"
	"io"
	"time"

	mcpapi "github.com/astralp2p/astral-go/api/mcp"
	"github.com/astralp2p/astral-go/astral"
	"github.com/astralp2p/astral-go/astral/channel"
	"github.com/astralp2p/astral-go/lib/query"
	"gorm.io/gorm"
)

// messagePollInterval is how often a waiting read_next looks again.
const messagePollInterval = 250 * time.Millisecond

// A caller reads its outbox row differently depending on whether the message is
// known not to be stored or merely not known to be, so delivery names the three
// outcomes apart.
var (
	errNotSent  = errors.New("the message did not leave this node")
	errRefused  = errors.New("the recipient's node refused it")
	errNoAnswer = errors.New("the message left and nothing came back")
)

// deliverMessage puts the message to the recipient and returns once the
// recipient's node has stored it.
//
// why a query and not a write to the table: a recipient on another node is the
// same call as one on this node, and routing is what tells them apart. The
// local case loops back through RouteQuery and takes the same path.
func (mod *Module) deliverMessage(agentID, targetID *astral.Identity, msg *mcpapi.Message) error {
	qctx, cancel := mod.ctx.WithIdentity(agentID).WithTimeout(mod.config.QueryTimeout)
	defer cancel()

	conn, err := query.RouteInFlight(qctx, mod.node,
		launch(query.New(agentID, targetID, mcpapi.MethodMessage, nil)))
	if err != nil {
		return fmt.Errorf("%w: %v", errNotSent, err)
	}
	defer conn.Close()

	// why the deadline: the recipient's node answers, but the link it answers
	// over can stall. Closing the conn is what unblocks the read.
	timer := time.AfterFunc(mod.config.QueryTimeout, func() { conn.Close() })
	defer timer.Stop()

	ch := channel.New(conn)

	if err = ch.Send(msg); err != nil {
		return fmt.Errorf("%w: %v", errNotSent, err)
	}

	// why a lost answer is not a failure: the recipient's node may have stored
	// the message and died before acknowledging it, so nothing here settles
	// whether the write happened.
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

// noteFetched records that a message's body was handed out, on whichever side
// holds the sender's row.
//
// why here and not inside db.ClaimNext: a remote sender is told by a query, and
// the database layer has no routing. Both branches belong where one can be.
func (mod *Module) noteFetched(row *dbMessage) {
	if mod.agentIDs.Contains(row.Sender.String()) {
		_ = mod.db.StampOutboxFetched(row.ID)
		return
	}

	// why the count gates the send: one attempt is made, and it belongs to
	// whichever read first handed the body out. A later read finds the row
	// already due and sends nothing.
	n, err := mod.db.MarkReceiptDue(row.ID)
	if err != nil || n == 0 {
		return
	}

	// why a goroutine and why nothing waits on it: the read is the recipient's
	// own, and it must not slow down or fail because the sender's node is
	// unreachable. A receipt is a courtesy; the fact it carries is already
	// true and durable here.
	go func(recipient, sender *astral.Identity, id mcpapi.MessageID) {
		if mod.sendReceipt(recipient, sender, id) == nil {
			_ = mod.db.StampReceiptStored(id)
		}
	}(row.Recipient, row.Sender, row.ID)
}

// sendReceipt tells the original sender's node that the body was handed out. It
// mirrors deliverMessage with the parties reversed: the recipient calls, and the
// sender is the target.
//
// One attempt is made. A receipt lost in transit leaves receipt_due_at set and
// nothing retries it, which leaves the sender believing a message was never
// collected — wrong, permanently, and in the direction that waits.
func (mod *Module) sendReceipt(recipientID, senderID *astral.Identity, id mcpapi.MessageID) error {
	qctx, cancel := mod.ctx.WithIdentity(recipientID).WithTimeout(mod.config.QueryTimeout)
	defer cancel()

	conn, err := query.RouteInFlight(qctx, mod.node,
		launch(query.New(recipientID, senderID, mcpapi.MethodReceipt, nil)))
	if err != nil {
		return err
	}
	defer conn.Close()

	timer := time.AfterFunc(mod.config.QueryTimeout, func() { conn.Close() })
	defer timer.Stop()

	ch := channel.New(conn)

	if err = ch.Send(&mcpapi.Receipt{ID: id}); err != nil {
		return err
	}

	obj, err := ch.Receive()
	if err != nil {
		return err
	}
	if _, ok := obj.(*astral.Ack); !ok {
		return fmt.Errorf("receipt answered %v", obj.ObjectType())
	}

	return nil
}

// acceptMessage answers a delivery: it reads one message off the conn, stores
// it in the recipient's inbox and acknowledges the write.
//
// why the node answers and not the agent: storing a message is a write, and it
// finishes inside the resolve deadline. The recipient's model is not part of
// delivery and need not be running.
func (mod *Module) acceptMessage(q *astral.InFlightQuery, w io.WriteCloser) (io.WriteCloser, error) {
	sender, recipient := q.Caller, q.Target

	return query.Accept(q, w, func(conn astral.Conn) {
		defer conn.Close()

		timer := time.AfterFunc(mod.config.QueryTimeout, func() { conn.Close() })
		defer timer.Stop()

		ch := channel.New(conn)

		obj, err := ch.Receive()
		if err != nil {
			return
		}

		msg, ok := obj.(*mcpapi.Message)
		if !ok {
			_ = ch.Send(astral.NewError("not a message"))
			return
		}

		if err = mod.storeMessage(sender, recipient, msg); err != nil {
			mod.log.Error("message for %v: %v", recipient, err)
			_ = ch.Send(astral.NewError(err.Error()))
			return
		}

		mod.log.Logv(1, "message %v from %v to %v", msg.ID, sender, recipient)

		_ = ch.Send(&astral.Ack{})
	})
}

// storeMessage writes a delivered message to the recipient's inbox.
//
// why the sender and the recipient come from the query and not the message:
// both are authenticated by the route, and a message that named them could
// name someone else.
func (mod *Module) storeMessage(sender, recipient *astral.Identity, msg *mcpapi.Message) error {
	switch {
	case msg.ID.IsZero():
		return errors.New("the message names no id")
	case len(msg.Content) > mod.config.MaxPayloadBytes:
		return errors.New("message too large")
	}

	return mod.db.InsertMessage(&dbMessage{
		ID:        msg.ID,
		Sender:    sender,
		Recipient: recipient,
		Content:   string(msg.Content),
	})
}

// claimNext claims the oldest unread message for the recipient, waiting up to
// timeout for one to arrive. Returns gorm.ErrRecordNotFound when the window
// closes with the inbox empty.
//
// why a poll and not a wake-up: the table already holds the answer, and a
// waiter woken by the writer has to be registered, unregistered and raced
// against a delivery landing between the two. A quarter of a second is under
// what an agent takes to read one message.
func (mod *Module) claimNext(ctx context.Context, recipient *astral.Identity, timeout time.Duration) (*dbMessage, error) {
	deadline := time.NewTimer(timeout)
	defer deadline.Stop()

	poll := time.NewTicker(messagePollInterval)
	defer poll.Stop()

	for {
		row, err := mod.db.ClaimNext(recipient)
		if !errors.Is(err, gorm.ErrRecordNotFound) {
			return row, err
		}

		select {
		case <-poll.C:
		case <-deadline.C:
			return nil, gorm.ErrRecordNotFound
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}
}
