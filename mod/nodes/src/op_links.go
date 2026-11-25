package nodes

import (
	"slices"

	"github.com/astralp2p/astral-go/api/auth"
	"github.com/astralp2p/astral-go/astral"
	"github.com/astralp2p/astral-go/astral/channel"
	"github.com/astralp2p/astral-go/lib/routing"
)

type opLinksArgs struct {
	Out string
}

// OpLinks lists all links.
func (mod *Module) OpLinks(ctx *astral.Context, q *routing.IncomingQuery, args opLinksArgs) (err error) {
	ch := channel.New(q.AcceptRaw(), channel.WithOutputFormat(args.Out))
	defer ch.Close()

	if !mod.Auth.Authorize(ctx, &auth.AdminNetworkAction{Action: auth.NewAction(q.Caller())}) {
		return ch.Send(auth.ErrNotPermitted)
	}

	links := mod.linkPool.links.Clone()

	slices.SortFunc(links, func(a, b *Link) int {
		return a.createdAt.Compare(b.createdAt)
	})

	for _, s := range links {
		err = ch.Send(s.Info())
		if err != nil {
			return ch.Send(astral.NewError(err.Error()))
		}
	}

	return ch.Send(&astral.EOS{})
}
