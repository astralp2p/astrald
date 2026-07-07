package fs

import (
	"github.com/cryptopunkscc/astral-go/astral"
	"github.com/cryptopunkscc/astral-go/astral/channel"
	"github.com/cryptopunkscc/astrald/lib/routing"
	"github.com/cryptopunkscc/astrald/mod/objects"
)

type opNewRepoArgs struct {
	Path  string
	Name  string
	Label string `query:"optional"`
	In    string `query:"optional"`
	Out   string `query:"optional"`
}

// OpNewRepo registers a new writable repository at the given path and adds it to the local group.
func (mod *Module) OpNewRepo(ctx *astral.Context, q *routing.IncomingQuery, args opNewRepoArgs) (err error) {
	ch := q.Accept(channel.WithFormats(args.In, args.Out))
	defer ch.Close()

	if args.Label == "" {
		args.Label = args.Name
	}

	var repo objects.Repository

	repo = NewRepository(mod, args.Name, args.Path)

	err = mod.Objects.AddRepository(args.Name, repo)
	if err != nil {
		return ch.Send(astral.Err(err))
	}

	err = mod.Objects.AddGroup(objects.RepoLocal, args.Name)
	if err != nil {
		return ch.Send(astral.Err(err))
	}

	return ch.Send(&astral.Ack{})
}
