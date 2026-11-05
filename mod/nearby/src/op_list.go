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

// OpList streams one Status per live cache entry: the verified node, nil for an
// unrecognised stealth status, and the attachments as received.
func (mod *Module) OpList(ctx *astral.Context, q *routing.IncomingQuery, args opListArgs) (err error) {
	ch := channel.New(q.AcceptRaw(), channel.WithOutputFormat(args.Out))
	defer ch.Close()

	if !mod.Auth.Authorize(ctx, &auth.AdminNetworkAction{Action: auth.NewAction(q.Caller())}) {
		return ch.Send(auth.ErrNotPermitted)
	}

	for _, e := range mod.cache.live(mod.now()) {
		err = ch.Send(&nearby.Status{
			Identity:    e.NodeID,
			Attachments: e.Status.Attachments,
		})
		if err != nil {
			return
		}
	}

	return ch.Send(&astral.EOS{})
}
