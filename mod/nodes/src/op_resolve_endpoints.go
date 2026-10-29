package nodes

import (
	"github.com/astralp2p/astral-go/api/auth"
	"github.com/astralp2p/astral-go/api/dir"
	"github.com/astralp2p/astral-go/astral"
	"github.com/astralp2p/astral-go/astral/channel"
	"github.com/astralp2p/astral-go/lib/routing"
)

type opResolveEndpointsArgs struct {
	Identity string `query:"required"`
	Out      string
}

func (mod *Module) OpResolveEndpoints(ctx *astral.Context, q *routing.IncomingQuery, args opResolveEndpointsArgs) (err error) {
	ch := channel.New(q.AcceptRaw(), channel.WithOutputFormat(args.Out))
	defer ch.Close()

	if !mod.Auth.Authorize(ctx, &auth.AdminNetworkAction{Action: auth.NewAction(q.Caller())}) {
		return ch.Send(auth.ErrNotPermitted)
	}

	// why: the zero identity names no node, and ResolveEndpoints fans the target
	// out to every registered resolver without testing it.
	targetID, err := mod.Dir.ResolveIdentity(args.Identity)
	if err != nil {
		return ch.Send(dir.ErrUnknownIdentity)
	}
	if targetID.IsZero() {
		return ch.Send(astral.ErrZeroIdentity)
	}

	endpoints, err := mod.ResolveEndpoints(ctx.WithIdentity(q.Caller()), targetID)
	if err != nil {
		mod.log.Error("resolve endpoints: %v", err)
		return ch.Send(astral.Err(err))
	}

	for endpoint := range endpoints {
		err = ch.Send(endpoint)
		if err != nil {
			return ch.Send(astral.NewError(err.Error()))
		}
	}

	return ch.Send(&astral.EOS{})
}
