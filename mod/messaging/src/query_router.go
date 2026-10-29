package messaging

import (
	"io"

	"github.com/astralp2p/astral-go/api/auth"
	"github.com/astralp2p/astral-go/api/messaging"
	"github.com/astralp2p/astral-go/astral"
	"github.com/astralp2p/astral-go/astral/channel"
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

	// why before hosting, and a refusal: a query from outside a send path —
	// see fromSendPath — to a target hosted elsewhere crosses a link, where the
	// far node cannot tell it from a send. This node is the only place it is
	// refused.
	if !fromSendPath(q) {
		origin := astral.OriginLocal
		if q.IsMCP() {
			origin = astral.OriginMCP
		}
		return query.Accept(q, w, func(conn astral.Conn) {
			defer conn.Close()
			ch := channel.New(conn)
			// why the request is read first: the caller writes it before it reads
			// the answer, and an unread write into this conn never returns.
			_, _ = ch.Receive()
			_ = ch.Send(astral.NewErrOriginNotAllowed(origin))
		})
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
	// why an answer and not a route miss: the target's mailbox is hosted here,
	// so the refusal is terminal, and an accepted delivery answered
	// messaging.ErrNotAdmitted is what tells a sender a refusal from an absence.
	// Which of the authority's reasons it was does not travel: the authority
	// answers one bit and the node holds no more.
	admitted := mod.Auth.Authorize(ctx, &messaging.ReceiveAction{
		Action: auth.NewAction(q.Target),
		FromID: q.Caller,
	})

	return mod.acceptMessage(q, w, admitted)
}
