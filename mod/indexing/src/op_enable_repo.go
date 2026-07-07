package indexing

import (
	modindexing "github.com/cryptopunkscc/astral-go/api/indexing"
	"github.com/cryptopunkscc/astral-go/astral"
	"github.com/cryptopunkscc/astral-go/astral/channel"
	"github.com/cryptopunkscc/astrald/lib/routing"
)

type opEnableRepoArgs struct {
	Repo    string
	Disable bool   `query:"optional"`
	In      string `query:"optional"`
	Out     string `query:"optional"`
}

// OpEnableRepo toggles repo indexing on or off; set Disable=true in args to
// deregister a previously enabled repo.
func (mod *Module) OpEnableRepo(ctx *astral.Context, q *routing.IncomingQuery, args opEnableRepoArgs) (err error) {
	ch := q.Accept(channel.WithFormats(args.In, args.Out))
	defer ch.Close()

	repo := mod.Objects.GetRepository(args.Repo)
	if repo == nil {
		return ch.Send(astral.Err(modindexing.ErrRepositoryNotFound))
	}

	if args.Disable {
		err = mod.DisableRepo(ctx, args.Repo)
	} else {
		err = mod.EnableRepo(ctx, args.Repo)
	}

	if err != nil {
		return ch.Send(astral.Err(err))
	}

	return ch.Send(&astral.Ack{})
}
