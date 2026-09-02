package mcp

import (
	"errors"
	"io"
	"time"

	"github.com/astralp2p/astral-go/api/mcp"
	"github.com/astralp2p/astral-go/astral"
	"github.com/astralp2p/astral-go/astral/channel"
	"github.com/astralp2p/astral-go/lib/query"
)

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

		msg, ok := obj.(*mcp.Message)
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
func (mod *Module) storeMessage(sender, recipient *astral.Identity, msg *mcp.Message) error {
	switch {
	case msg.ID.IsZero():
		return errors.New("the message names no id")
	case len(msg.Content) > mod.config.MaxPayloadBytes:
		return errors.New("message too large")
	case msg.ParentID == msg.ID:
		// A message answering itself is a cycle of one, the cheapest to refuse.
		return errors.New("a message may not answer itself")
	}

	// why a parent this node does not hold is refused, where it was once kept:
	// a reply may only extend a conversation its two parties share. The
	// recipient must hold the parent here, the sender must hold it in
	// sendMessage, and a message has one of each — so the parent is a message
	// between exactly these two, and an agent cannot reply into a conversation
	// it is not part of. The same rule makes a cycle unstorable: every parent
	// then points at a row stored earlier, so the graph is a forest no walker
	// loops through, where a bare claim let a wire sender cross-reference two
	// fresh ids into a loop a reader without a seen-set never leaves.
	if !msg.ParentID.IsZero() {
		held, err := mod.db.Holds(recipient, msg.ParentID)
		if err != nil {
			return err
		}
		if !held {
			return errors.New("the message answers one this node does not hold")
		}
	}

	n, err := mod.db.InsertInbox(&dbMessage{
		ID:        msg.ID,
		Sender:    sender,
		Recipient: recipient,
		Content:   string(msg.Content),
		ParentID:  msg.ParentID,
	})
	if err != nil {
		return err
	}
	if n == 1 {
		// why the wake is here and not on the way out: the row is committed,
		// and the path below wrote nothing for anyone to be woken about. A wake
		// on "no error" would fire for a delivery that stored nothing, which is
		// the same conflation InsertInbox's RowsAffected exists to undo.
		mod.waiters.wake(recipient)
		return nil
	}

	// Nothing was written, and two different things look like this: the sender
	// repeating a delivery whose acknowledgement it never saw, and a second
	// sender minting an id this inbox already holds. The id is the sender's to
	// choose, so the second is reachable and must not be answered with an ack
	// the recipient will never see a message for.
	held, err := mod.db.SenderOf(recipient, boxInbox, msg.ID)
	if err != nil {
		return err
	}
	if !held.IsEqual(sender) {
		return errors.New("a message is already stored under that id")
	}

	return nil
}
