package fs

import (
	"github.com/cryptopunkscc/astral-go/api/objects"
	"github.com/cryptopunkscc/astral-go/astral"
	"github.com/cryptopunkscc/astral-go/astral/channel"
	"github.com/cryptopunkscc/astral-go/lib/routing"
)

type opNewWatchArgs struct {
	Path  string
	Name  string
	Label string `query:"optional"`
	In    string `query:"optional"`
	Out   string `query:"optional"`
}

// OpNewWatch registers a new watched repository at the given path, starts an async background scan,
// and adds the repository to both the objects store and the local group.
func (mod *Module) OpNewWatch(ctx *astral.Context, q *routing.IncomingQuery, args opNewWatchArgs) (err error) {
	ch := q.Accept(channel.WithFormats(args.In, args.Out))
	defer ch.Close()

	if args.Label == "" {
		args.Label = args.Name
	}

	repo, err := NewWatchRepository(mod, args.Path, args.Label)
	if err != nil {
		return ch.Send(astral.Err(err))
	}

	scanCtx, cancel := mod.ctx.WithCancel()
	repo.scanCancel = cancel

	go func() {
		if err := mod.indexer.scan(scanCtx, args.Path, true); err != nil {
			mod.log.Error("scan %v: %v", args.Path, err)
		}
	}()

	err = mod.Objects.AddRepository(args.Name, repo)
	if err != nil {
		cancel()
		return ch.Send(astral.Err(err))
	}

	err = mod.Objects.AddGroup(objects.RepoLocal, args.Name)
	if err != nil {
		cancel()
		return ch.Send(astral.Err(err))
	}

	return ch.Send(&astral.Ack{})
}
