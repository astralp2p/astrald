package ip

import (
	"github.com/cryptopunkscc/astral-go/astral"
	"github.com/cryptopunkscc/astral-go/astral/channel"
	"github.com/cryptopunkscc/astrald/lib/routing"
)

type opPublicIPCandidatesArgs struct {
	Out string `query:"optional"`
}

func (mod *Module) OpPublicIPCandidates(ctx *astral.Context, q *routing.IncomingQuery, args opPublicIPCandidatesArgs) (err error) {
	ch := channel.New(q.AcceptRaw(), channel.WithOutputFormat(args.Out))
	defer ch.Close()

	for _, addr := range mod.PublicIPCandidates() {
		err = ch.Send(&addr)
		if err != nil {
			return ch.Send(astral.NewError(err.Error()))
		}
	}

	return ch.Send(&astral.EOS{})

}
