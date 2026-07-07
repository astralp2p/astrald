package user

import (
	"github.com/cryptopunkscc/astral-go/astral"
	"github.com/cryptopunkscc/astral-go/astral/channel"
	"github.com/cryptopunkscc/astrald/lib/routing"
)

type opListSiblingsArgs struct {
	Out  string      `query:"optional"`
	Zone astral.Zone `query:"optional"`
}

// OpListSiblings streams the identities of all currently linked sibling nodes.
// Derives the context from the caller's identity and the requested zone.
func (mod *Module) OpListSiblings(ctx *astral.Context, q *routing.IncomingQuery, args opListSiblingsArgs) (err error) {
	ctx, cancel := ctx.WithIdentity(q.Caller()).IncludeZone(args.Zone).WithCancel()
	defer cancel()

	ch := channel.New(q.AcceptRaw(), channel.WithOutputFormat(args.Out))
	defer ch.Close()

	for _, id := range mod.getSiblings() {
		err = ch.Send(id)
		if err != nil {
			return ch.Send(astral.NewError(err.Error()))
		}
	}

	return ch.Send(&astral.EOS{})
}
