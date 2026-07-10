package nearby

import (
	"github.com/astralp2p/astral-go/astral"
	"github.com/astralp2p/astral-go/astral/channel"
	"github.com/astralp2p/astral-go/lib/routing"
	"github.com/astralp2p/astrald/mod/nearby"
)

type opListArgs struct {
	Out string `query:"optional"`
}

func (mod *Module) OpList(ctx *astral.Context, q *routing.IncomingQuery, args opListArgs) (err error) {
	ch := channel.New(q.AcceptRaw(), channel.WithOutputFormat(args.Out))
	defer ch.Close()

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
