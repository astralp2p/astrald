package mcp

import (
	"fmt"
	"time"

	"github.com/astralp2p/astral-go/api/mcp"
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
func (mod *Module) deliverMessage(agentID, targetID *astral.Identity, msg *mcp.Message) error {
	qctx, cancel := mod.ctx.WithIdentity(agentID).WithTimeout(mod.config.QueryTimeout)
	defer cancel()

	conn, err := query.RouteInFlight(qctx, mod.node,
		launch(query.New(agentID, targetID, mcp.MethodMessage, nil)))
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
