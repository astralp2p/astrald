package nearby

import (
	"github.com/astralp2p/astral-go/api/auth"
	"github.com/astralp2p/astral-go/api/nearby"
	"github.com/astralp2p/astral-go/astral"
	"github.com/astralp2p/astral-go/astral/channel"
	"github.com/astralp2p/astral-go/lib/routing"
)

type opListArgs struct {
	Out string
}

func (mod *Module) OpList(ctx *astral.Context, q *routing.IncomingQuery, args opListArgs) (err error) {
	ch := channel.New(q.AcceptRaw(), channel.WithOutputFormat(args.Out))
	defer ch.Close()

	if !mod.Auth.Authorize(ctx, &auth.AdminNetworkAction{Action: auth.NewAction(q.Caller())}) {
		return ch.Send(auth.ErrNotPermitted)
	}

	for _, v := range mod.Cache().Clone() {
		err = ch.Send(&nearby.Status{
			Identity:    mod.ResolveStatus(v.Status),
			Attachments: v.Status.Attachments,
		})
		if err != nil {
			return
		}
	}

	return ch.Send(&astral.EOS{})
}
