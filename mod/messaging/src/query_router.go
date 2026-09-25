package messaging

import (
	"io"

	"github.com/astralp2p/astral-go/api/auth"
	"github.com/astralp2p/astral-go/api/messaging"
	"github.com/astralp2p/astral-go/astral"
	"github.com/astralp2p/astral-go/lib/query"
)

// RouteQuery answers a delivery or a receipt addressed to a mailbox this node
// hosts.
//
// The node holds no reachability of its own. It hosts many tenants' mailboxes
// and knows no relation between them, so which callers a participant answers is
// asked of auth and never decided here.
func (mod *Module) RouteQuery(ctx *astral.Context, q *astral.InFlightQuery, w io.WriteCloser) (io.WriteCloser, error) {
	path, _ := query.Parse(q.QueryString.String())

	// why every other path is a miss, asked before anything else: a mailbox is
	// not a service, and hosting one for the target claims no other query
	// addressed to it. The other routers try it as if this module were absent.
	if path != messaging.MethodMessage && path != messaging.MethodReceipt {
		return query.RouteNotFound()
	}

	// why before hosting, and a rejection: a query from outside a send path —
	// see fromSendPath — to a target hosted elsewhere crosses a link, where the
	// far node cannot tell it from a send. This node is the only place it is
	// refused.
	if !fromSendPath(q) {
		return query.Reject()
	}

	// why hosting before the correspondent: only a mailbox this node hosts is
	// this module's to answer for, and every other target must fall through to
	// the other routers — without reaching an authority that has nothing to say
	// about who writes to it.
	if !mod.hosts(q.Target) {
		return query.RouteNotFound()
	}

	// why a receipt is admitted without asking the authority: the outbox row is
	// the permission, and permissions are granted per side — asking
	// receive_action would refuse a receipt whenever the two differ.
	if path == messaging.MethodReceipt {
		return mod.acceptReceipt(q, w)
	}

	// why the actor is the target and not the caller: auth walks the contracts
	// the actor is subject to, and taking a message is this participant's act.
	//
	// why a rejection and not a route miss: the target's mailbox is hosted
	// here, so the answer is terminal, and a code is what tells a sender a
	// refusal from an absence. Which of the authority's reasons it was does not
	// travel: the authority answers one bit and the node holds no more.
	if !mod.Auth.Authorize(ctx, &messaging.ReceiveAction{
		Action: auth.NewAction(q.Target),
		FromID: q.Caller,
	}) {
		return query.RejectWithCode(messaging.RejectNotAdmitted)
	}

	return mod.acceptMessage(q, w)
}
