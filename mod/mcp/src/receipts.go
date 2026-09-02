package mcp

import (
	"fmt"
	"io"
	"time"

	"github.com/astralp2p/astral-go/api/mcp"
	"github.com/astralp2p/astral-go/astral"
	"github.com/astralp2p/astral-go/astral/channel"
	"github.com/astralp2p/astral-go/lib/query"
)

// noteFetched records that a message's body was handed out, on whichever side
// holds the sender's row.
//
// why here and not in the store: a remote sender is told by a query, and the
// database layer has no routing.
func (mod *Module) noteFetched(m *mcp.StoredMessage) {
	// why the box is checked first: only handing out an inbox body is a
	// collection. Stamping an agent's own sent row would tell it that someone
	// collected a message it merely re-read.
	if m.Box != mcp.BoxInbox {
		return
	}

	if mod.agentIDs.Contains(m.Sender.String()) {
		if err := mod.db.StampFetched(m.Sender, m.ID); err != nil {
			mod.log.Error("outbox %v: stamping fetched_at: %v", m.ID, err)
		}
		return
	}

	// why the count gates the send: one attempt is made, and it belongs to the
	// read that first handed the body out. The error is a separate branch
	// because a failed write and a receipt already owed both send nothing.
	n, err := mod.db.MarkReceiptDue(m.Recipient, m.ID)
	if err != nil {
		mod.log.Error("message %v: marking the receipt due: %v", m.ID, err)
		return
	}
	if n == 0 {
		return
	}

	// why nothing waits on this: the read is the recipient's own and must not
	// fail because the sender's node is unreachable. The fact a receipt carries
	// is already durable here.
	go func(recipient, sender *astral.Identity, id mcp.MessageID) {
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
	}(m.Recipient, m.Sender, m.ID)
}

// sendReceipt tells the original sender's node that the body was handed out. It
// mirrors deliverMessage with the parties reversed: the recipient calls, and the
// sender is the target.
//
// One attempt is made: a receipt lost in transit leaves receipt_due_at set and
// the sender believing the message was never collected.
func (mod *Module) sendReceipt(recipientID, senderID *astral.Identity, id mcp.MessageID) error {
	qctx, cancel := mod.ctx.WithIdentity(recipientID).WithTimeout(mod.config.QueryTimeout)
	defer cancel()

	conn, err := query.RouteInFlight(qctx, mod.node,
		launch(query.New(recipientID, senderID, mcp.MethodReceipt, nil)))
	if err != nil {
		return err
	}
	defer conn.Close()

	timer := time.AfterFunc(mod.config.QueryTimeout, func() { conn.Close() })
	defer timer.Stop()

	ch := channel.New(conn)

	if err = ch.Send(&mcp.Receipt{ID: id}); err != nil {
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

		r, ok := obj.(*mcp.Receipt)
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
