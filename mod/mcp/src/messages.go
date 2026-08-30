package mcp

import (
	"context"
	"errors"
	"fmt"
	"io"
	"time"

	authapi "github.com/astralp2p/astral-go/api/auth"
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

// sendMessage puts one message to a recipient and records what became of it.
// It answers the id the message was stored under and the thread it went into.
//
// why the thread comes back: a sender that named none started one, and it
// cannot follow the answer without learning the name.
//
// why the two refusals answer the same words: an agent learns that it cannot
// reach this recipient, and not whether the recipient exists.
// The thread is the sender's: a message naming none is the root of its own
// exchange, and a reply names the thread it answers.
func (mod *Module) sendMessage(agentID *astral.Identity, to, content string, thread mcpapi.MessageID) (id, sent mcpapi.MessageID, err error) {
	targetID, err := mod.Dir.ResolveIdentity(to)
	if err != nil {
		return id, sent, fmt.Errorf("unknown recipient: %v", to)
	}

	// The same question astral-query asks about the same pair: what this agent
	// may reach is its owner's decision, and a message buys it no reach it did
	// not have.
	if !mod.Auth.Authorize(mod.ctx, &mcpapi.CallAgentAction{
		Action: authapi.NewAction(agentID),
		ToID:   targetID,
	}) {
		return id, sent, fmt.Errorf("unknown recipient: %v", to)
	}

	if len(content) > mod.config.MaxPayloadBytes {
		return id, sent, fmt.Errorf("content is over %v bytes", mod.config.MaxPayloadBytes)
	}

	msg := &mcpapi.Message{
		ID:      mcpapi.NewMessageID(),
		Content: astral.String32(content),
		Thread:  thread,
	}
	if msg.Thread.IsZero() {
		msg.Thread = msg.ID
	}

	// why the row is written here and nothing above it is: a stored list of
	// refusals would tell a recipient that refuses apart from one that does not
	// exist, which is the collapse the two refusals above are built on.
	if err = mod.db.InsertOutbox(&dbOutbox{
		ID:        msg.ID,
		Sender:    agentID,
		Recipient: targetID,
		Content:   content,
		Thread:    msg.Thread,
	}); err != nil {
		return id, sent, err
	}

	if err = mod.deliverMessage(agentID, targetID, msg); err != nil {
		// why errNoAnswer stamps nothing: an answer that never arrived proves
		// nothing about the write, and the row that says nothing is the row
		// that is right.
		//
		// why a stamp that fails is logged and never returned: the delivery
		// already happened as it happened, so failing the caller would deny
		// it. Every state here is read off which instants are set, so a lost
		// stamp is a row claiming a fact that did occur never did.
		switch {
		case errors.Is(err, errRefused):
			if serr := mod.db.StampOutboxFailed(msg.ID); serr != nil {
				mod.log.Error("outbox %v: stamping failed_at: %v", msg.ID, serr)
			}
			if serr := mod.db.SetOutboxErr(msg.ID, err.Error()); serr != nil {
				mod.log.Error("outbox %v: recording the refusal: %v", msg.ID, serr)
			}
		case errors.Is(err, errNotSent):
			if serr := mod.db.StampOutboxFailed(msg.ID); serr != nil {
				mod.log.Error("outbox %v: stamping failed_at: %v", msg.ID, serr)
			}
		}

		// why the caller is told the same words in all three cases: which one
		// happened is a fact about the row, and the outbox is what answers it.
		return id, sent, fmt.Errorf("delivery failed: %v", err)
	}

	if err = mod.db.StampOutboxStored(msg.ID); err != nil {
		mod.log.Error("outbox %v: stamping stored_at: %v", msg.ID, err)
	}

	return msg.ID, msg.Thread, nil
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
		if err := mod.db.StampOutboxFetched(row.ID); err != nil {
			mod.log.Error("outbox %v: stamping fetched_at: %v", row.ID, err)
		}
		return
	}

	// why the count gates the send: one attempt is made, and it belongs to
	// whichever read first handed the body out. A later read finds the row
	// already due and sends nothing.
	//
	// why the error is its own branch: a write that failed and a receipt
	// already owed both send nothing, and only one of them is a fault.
	n, err := mod.db.MarkReceiptDue(row.ID)
	if err != nil {
		mod.log.Error("message %v: marking the receipt due: %v", row.ID, err)
		return
	}
	if n == 0 {
		return
	}

	// why a goroutine and why nothing waits on it: the read is the recipient's
	// own, and it must not slow down or fail because the sender's node is
	// unreachable. A receipt is a courtesy; the fact it carries is already
	// true and durable here.
	go func(recipient, sender *astral.Identity, id mcpapi.MessageID) {
		// why the failure is logged and not returned: nothing waits on this
		// goroutine, and one attempt is made. The line is the only account of
		// a receipt that never arrived.
		if err := mod.sendReceipt(recipient, sender, id); err != nil {
			mod.log.Error("receipt %v to %v: %v", id, sender, err)
			return
		}
		if err := mod.db.StampReceiptStored(id); err != nil {
			mod.log.Error("message %v: stamping receipt_stored_at: %v", id, err)
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

// acceptReceipt answers a receipt: it reads one off the conn and stamps the
// sender's outbox row collected.
//
// The row is the admission. A caller naming a message this agent never sent it
// is answered an error, and a node holding no such row learns nothing about why.
func (mod *Module) acceptReceipt(q *astral.InFlightQuery, w io.WriteCloser) (io.WriteCloser, error) {
	// reversed: we are the original sender, the caller is the recipient
	sender, recipient := q.Target, q.Caller

	return query.Accept(q, w, func(conn astral.Conn) {
		defer conn.Close()

		timer := time.AfterFunc(mod.config.QueryTimeout, func() { conn.Close() })
		defer timer.Stop()

		ch := channel.New(conn)

		obj, err := ch.Receive()
		if err != nil {
			return
		}

		r, ok := obj.(*mcpapi.Receipt)
		if !ok {
			_ = ch.Send(astral.NewError("not a receipt"))
			return
		}

		n, err := mod.db.StampOutboxFetchedFrom(sender, recipient, r.ID)
		if err != nil || n == 0 {
			_ = ch.Send(astral.NewError("unknown message"))
			return
		}

		mod.log.Logv(1, "receipt %v from %v to %v", r.ID, recipient, sender)

		_ = ch.Send(&astral.Ack{})
	})
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

	// why the thread is settled here and not taken as sent: a peer on a node
	// that predates the field names none, and every message must be in a
	// thread for a reader to rely on one. A message naming no exchange is the
	// root of its own, which is what every message was before threads existed.
	thread := msg.Thread
	if thread.IsZero() {
		thread = msg.ID
	}

	return mod.db.InsertMessage(&dbMessage{
		ID:        msg.ID,
		Sender:    sender,
		Recipient: recipient,
		Content:   string(msg.Content),
		Thread:    thread,
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
func (mod *Module) claimNext(ctx context.Context, recipient *astral.Identity, q inboxQuery, timeout time.Duration) (*dbMessage, error) {
	deadline := time.NewTimer(timeout)
	defer deadline.Stop()

	poll := time.NewTicker(messagePollInterval)
	defer poll.Stop()

	for {
		row, err := mod.db.ClaimNext(recipient, q)
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
