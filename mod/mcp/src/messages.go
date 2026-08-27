package mcp

import (
	"context"
	"crypto/rand"
	"encoding/hex"
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

// maxMessageIDLen bounds an identifier a sender mints. The identifier is a
// name, and a sender that sends a body in its place is refused.
const maxMessageIDLen = 64

// newMessageID mints the identifier a message carries on both sides.
//
// why 128 bits: the identifier names the message in every inbox that keeps it
// and in every reply that answers it, so it competes against every message the
// node has stored rather than against the ones in flight. 64 bits reaches a
// one-in-a-million collision at six million messages, which a node outlives.
func newMessageID() string {
	var b [16]byte
	_, _ = rand.Read(b[:])
	return hex.EncodeToString(b[:])
}

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
		return err
	}
	defer conn.Close()

	// why the deadline: the recipient's node answers, but the link it answers
	// over can stall. Closing the conn is what unblocks the read.
	timer := time.AfterFunc(mod.config.QueryTimeout, func() { conn.Close() })
	defer timer.Stop()

	ch := channel.New(conn)

	if err = ch.Send(msg); err != nil {
		return err
	}

	obj, err := ch.Receive()
	if err != nil {
		return err
	}

	if e, ok := obj.(astral.Error); ok {
		return errors.New(e.Error())
	}
	if _, ok := obj.(*astral.Ack); !ok {
		return fmt.Errorf("delivery answered %v", obj.ObjectType())
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
	case msg.ID == "" || len(msg.ID) > maxMessageIDLen:
		return errors.New("message id")
	case len(msg.Content) > mod.config.MaxPayloadBytes:
		return errors.New("message too large")
	}

	return mod.db.InsertMessage(&dbMessage{
		ID:        string(msg.ID),
		Sender:    sender,
		Recipient: recipient,
		Topic:     string(msg.Topic),
		Content:   string(msg.Content),
		ReplyTo:   string(msg.ReplyTo),
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
