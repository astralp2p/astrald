package nearby

import (
	"github.com/cryptopunkscc/astral-go/astral"
	"github.com/cryptopunkscc/astral-go/astral/channel"
	"github.com/cryptopunkscc/astrald/lib/routing"
	"github.com/cryptopunkscc/astrald/mod/nearby"
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
