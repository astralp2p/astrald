package nodes

import (
	"github.com/astralp2p/astral-go/astral"
	"github.com/astralp2p/astral-go/astral/channel"
	"github.com/astralp2p/astral-go/lib/routing"
)

type opResolveEndpointsArgs struct {
	Identity string `query:"required"`
	Out      string
}

func (mod *Module) OpResolveEndpoints(ctx *astral.Context, q *routing.IncomingQuery, args opResolveEndpointsArgs) (err error) {
	if !mod.authorizeAdminNetwork(ctx, q) {
		return q.Reject()
	}

	// why: the zero identity names no node, and ResolveEndpoints fans the target
	// out to every registered resolver without testing it.
	targetID, err := mod.Dir.ResolveIdentity(args.Identity)
	if err != nil || targetID.IsZero() {
		return q.RejectWithCode(2)
	}

	endpoints, err := mod.ResolveEndpoints(ctx.WithIdentity(q.Caller()), targetID)
	if err != nil {
		mod.log.Error("resolve endpoints: %v", err)
		return q.RejectWithCode(astral.CodeInternalError)
	}

	ch := channel.New(q.AcceptRaw(), channel.WithOutputFormat(args.Out))
	defer ch.Close()

	for endpoint := range endpoints {
		err = ch.Send(endpoint)
		if err != nil {
			return ch.Send(astral.NewError(err.Error()))
		}
	}

	return ch.Send(&astral.EOS{})
}
