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
)

// waitFloor is how often a parked wait looks again on its own.
//
// why it exists at all, given the wake: the wake is only as good as the set of
// statements that remember to fire it, and that set grows — a repair, an
// import, a tool not yet written. Without a floor a missed wake is not a slow
// answer, it is a wrong one: pollMessages returns nil on its deadline and the
// tool reports timed_out, so an agent is told the window closed with nothing
// new while unarchived mail sits in its inbox. That is the one answer this
// design exists to make impossible.
//
// why ten seconds: as the way a waiter normally learns, 250ms was right and
// cost 0.03% of a core per parked agent. As a backstop it is forty times more
// idle work than the job needs. What the interval has to satisfy is that it is
// comfortably under the default window (Config.WaitDefault), so an unnamed
// park catches a missed wake before its deadline can report timed_out over a
// non-empty inbox. A park the caller shortened below the floor leans on the
// wake alone, which covers every writer this module has.
const waitFloor = 10 * time.Second

// A caller reads its outbox row differently depending on whether the message is
// known not to be stored or merely not known to be, so delivery names the three
// outcomes apart.
var (
	errNotSent  = errors.New("the message did not leave this node")
	errRefused  = errors.New("the recipient's node refused it")
	errNoAnswer = errors.New("the message left and nothing came back")
)

// sendMessage puts one message to a recipient and records what became of it.
// It answers the id the message was stored under, which is also the value a
// later message names as its parent.
func (mod *Module) sendMessage(agentID *astral.Identity, to, content string, parent mcpapi.MessageID) (id mcpapi.MessageID, err error) {
	targetID, err := mod.resolveRecipient(agentID, to)
	if err != nil {
		return id, err
	}

	if len(content) > mod.config.MaxPayloadBytes {
		return id, fmt.Errorf("content is over %v bytes", mod.config.MaxPayloadBytes)
	}

	// why a reply to a message this agent does not hold is refused here too:
	// the recipient's node refuses a parent it does not hold, so a send that
	// named one this agent cannot itself see would write an outbox row and then
	// fail delivery. Refusing it here answers the agent at once, and keeps the
	// agent's own thread graph the forest the recipient's is.
	if !parent.IsZero() {
		held, err := mod.db.Holds(agentID, parent)
		if err != nil {
			return id, err
		}
		if !held {
			return id, errors.New("cannot answer a message you do not hold")
		}
	}

	msg := &mcpapi.Message{
		ID:       mcpapi.NewMessageID(),
		Content:  astral.String32(content),
		ParentID: parent,
	}

	// why the row is written here and nothing above it is: a stored list of
	// refusals would tell a recipient that refuses apart from one that does not
	// exist, which is the collapse resolveRecipient is built on.
	if err = mod.db.InsertOutbox(&dbMessage{
		ID:        msg.ID,
		Sender:    agentID,
		Recipient: targetID,
		Content:   content,
		ParentID:  parent,
	}); err != nil {
		return id, err
	}

	if err = mod.deliverMessage(agentID, targetID, msg); err != nil {
		mod.noteDeliveryFailed(agentID, msg.ID, err)

		// why the caller is told the same words in all three cases: which one
		// happened is a fact about the row, and the sent list is what answers it.
		return id, fmt.Errorf("delivery failed: %v", err)
	}

	if err = mod.db.StampLanded(agentID, msg.ID); err != nil {
		mod.log.Error("outbox %v: stamping landed_at: %v", msg.ID, err)
	}

	return msg.ID, nil
}

// resolveRecipient answers who a name means, if this agent may reach them.
//
// why all three refusals answer the same words: an agent learns that it cannot
// reach this recipient, and not whether the recipient exists.
func (mod *Module) resolveRecipient(agentID *astral.Identity, to string) (*astral.Identity, error) {
	unknown := fmt.Errorf("unknown recipient: %v", to)

	targetID, err := mod.Dir.ResolveIdentity(to)
	if err != nil {
		return nil, unknown
	}

	// why the empty name is refused here: ResolveIdentity answers the Anyone
	// identity for an empty string rather than an error, and Anyone is a target
	// the authorizer and the router would both accept.
	if targetID.IsZero() {
		return nil, unknown
	}

	// The same question astral-query asks about the same pair: what this agent
	// may reach is its owner's decision, and a message buys it no reach it did
	// not have.
	if !mod.Auth.Authorize(mod.ctx, &mcpapi.CallAgentAction{
		Action: authapi.NewAction(agentID),
		ToID:   targetID,
	}) {
		return nil, unknown
	}

	return targetID, nil
}

// noteDeliveryFailed records what became of a send that did not land.
//
// why errNoAnswer stamps nothing: an answer that never arrived proves nothing
// about the write, and the row that says nothing is the row that is right.
//
// why a stamp that fails is logged and never returned: the delivery already
// happened as it happened, so failing the caller would deny it. Every state
// here is read off which instants are set, so a lost stamp is a row claiming a
// fact that did occur never did.
func (mod *Module) noteDeliveryFailed(agentID *astral.Identity, id mcpapi.MessageID, cause error) {
	if !errors.Is(cause, errRefused) && !errors.Is(cause, errNotSent) {
		return
	}

	if err := mod.db.StampFailed(agentID, id); err != nil {
		mod.log.Error("outbox %v: stamping failed_at: %v", id, err)
	}

	if !errors.Is(cause, errRefused) {
		return
	}
	if err := mod.db.SetErr(agentID, id, cause.Error()); err != nil {
		mod.log.Error("outbox %v: recording the refusal: %v", id, err)
	}
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
	// why the box is checked first: an agent reading its own sent row is
	// reading its own ledger, and stamping fetched_at there would tell it that
	// someone collected a message it merely re-read. Only handing out an inbox
	// body is a collection.
	if row.Box != boxInbox {
		return
	}

	if mod.agentIDs.Contains(row.Sender.String()) {
		if err := mod.db.StampFetched(row.Sender, row.ID); err != nil {
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
	n, err := mod.db.MarkReceiptDue(row.Recipient, row.ID)
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
		if err := mod.db.StampReceiptStored(recipient, id); err != nil {
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

		// why n == 1 and not n != 0: the owner and the box narrow this to one
		// row, so any other count is a predicate that widened, not a receipt
		// that matched.
		n, err := mod.db.StampFetchedFrom(sender, recipient, r.ID)
		if err != nil || n != 1 {
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

// waitForMessages parks until the owner's chosen list holds a message the query
// matches, and answers what it found. Nothing is stamped and nothing is taken:
// two agents waiting at once are answered the same messages, and a reader that
// stops between the answer and the work leaves the mailbox as it was.
//
// why a poll and not a wake-up: the table already holds the answer, and a
// waiter woken by the writer has to be registered, unregistered and raced
// against a delivery landing between the two. A quarter of a second is under
// what an agent takes to read one message.
func (mod *Module) pollMessages(ctx context.Context, owner *astral.Identity, q messageQuery, timeout time.Duration) ([]dbMessage, error) {
	deadline := time.NewTimer(timeout)
	defer deadline.Stop()

	// why the registration precedes the first look: a row landing between the
	// query and the subscribe is one the waiter would sleep through, because
	// the writer signals into a registry this waiter is not yet in — the token
	// is not dropped, it is never made. Registering first and buffering the
	// channel close two different windows, and either one alone still loses
	// wakes.
	woke, leave := mod.waiters.park(owner)
	defer leave()

	floor := time.NewTicker(waitFloor)
	defer floor.Stop()

	for {
		rows, err := mod.db.ListMessages(owner, q)
		if err != nil {
			return nil, err
		}
		if len(rows) > 0 {
			return rows, nil
		}

		select {
		case <-woke:
		case <-floor.C:
		case <-deadline.C:
			return nil, nil
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}
}
