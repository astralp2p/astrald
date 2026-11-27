package user

import (
	"io"
	"time"

	"github.com/astralp2p/astral-go/astral"
	"github.com/astralp2p/astral-go/astral/channel"
	"github.com/astralp2p/astral-go/lib/query"
	usermod "github.com/astralp2p/astrald/mod/user"
)

// swarmPush is one entry a test node offered to a peer's swarm_push.
type swarmPush struct {
	target   *astral.Identity
	object   astral.Object
	zone     astral.Zone
	deadline time.Time
}

// sameObject reports whether a and b have one object ID: a push crosses the router as a decoded copy.
func sameObject(a, b astral.Object) bool {
	ida, erra := astral.ResolveObjectID(a)
	idb, errb := astral.ResolveObjectID(b)
	return erra == nil && errb == nil && ida.IsEqual(idb)
}

// swarmPeerNode is a node whose router serves the exchange ops of every peer: it hands each swarm_push to
// onPush and answers err, or an Ack; swarm_digest and swarm_fetch answer nothing.
type swarmPeerNode struct {
	identityNode
	onPush func(swarmPush)
	err    astral.Error
}

func (n *swarmPeerNode) RouteQuery(ctx *astral.Context, q *astral.InFlightQuery, w io.WriteCloser) (io.WriteCloser, error) {
	op, _ := query.Parse(q.QueryString.String())
	zone := ctx.Zone()
	deadline, _ := ctx.Deadline()

	return query.Accept(q, w, func(conn astral.Conn) {
		ch := channel.New(conn)
		defer ch.Close()

		if op != usermod.ModuleName+"."+opSwarmPush {
			ch.Send(&astral.EOS{})
			return
		}
		obj, err := ch.Receive()
		if err != nil {
			return
		}
		n.onPush(swarmPush{target: q.Target, object: obj, zone: zone, deadline: deadline})
		if n.err != nil {
			ch.Send(n.err)
			return
		}
		ch.Send(&astral.Ack{})
	})
}
