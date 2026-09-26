package messaging

import (
	"github.com/astralp2p/astral-go/api/auth"
	"github.com/astralp2p/astral-go/api/messaging"
	"github.com/astralp2p/astral-go/astral"
	"github.com/astralp2p/astral-go/lib/routing"
)

// refusesOrigin answers whether the query came from where no messaging
// operation is served: over a link, or from an agent over MCP.
//
// why the MCP origin is refused here as well as in mod/shell: shell refuses it
// only where it mounts this router, and an operation that trusted its mount
// point would serve an agent wherever the router is mounted next.
func refusesOrigin(q *routing.IncomingQuery) bool {
	return q.Origin() == astral.OriginNetwork || q.Origin() == astral.OriginMCP
}

// admitsMailCaller answers whether send_message, wait or archive serves this
// query: an origin this module serves, and a caller whose mailbox this node
// hosts.
//
// why the caller alone names the mailbox: sending, waiting and archiving act
// on the caller's own boxes, so no value a caller sends can reach another
// participant's. Only a read names another mailbox — see admitsReader.
func (mod *Module) admitsMailCaller(q *routing.IncomingQuery) bool {
	return !refusesOrigin(q) && mod.hosts(q.Caller())
}

// mayRead answers whether the caller may be a reader at all: neither the zero
// identity nor this node.
//
// why the node is refused by name: an anonymous local caller arrives as the
// node identity, so a read granted to the node would be granted to every
// anonymous caller.
func (mod *Module) mayRead(caller *astral.Identity) bool {
	return !caller.IsZero() && !caller.IsEqual(mod.node.Identity())
}

// admitsReader answers whether a delegated read serves the caller on the
// mailbox it names, checked in this order: the caller may read at all, this
// node hosts the mailbox, and auth grants the caller
// mod.messaging.read_mailbox_action on it.
//
// why the caller need not be hosted here: the grant is the reader's authority,
// and reading uses no mailbox of the reader's own.
//
// why hosting is checked before auth is asked: a mailbox this node does not
// serve is refused whatever the authority would answer, and asking would put a
// question about it to an external authority for nothing.
func (mod *Module) admitsReader(ctx *astral.Context, caller, mailbox *astral.Identity) bool {
	if !mod.mayRead(caller) || !mod.hosts(mailbox) {
		return false
	}

	return mod.Auth.Authorize(ctx, &messaging.ReadMailboxAction{
		Action:    auth.NewAction(caller),
		MailboxID: mailbox,
	})
}
