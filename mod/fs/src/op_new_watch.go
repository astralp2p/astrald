package fs

import (
	"github.com/astralp2p/astral-go/api/auth"
	"github.com/astralp2p/astral-go/api/objects"
	"github.com/astralp2p/astral-go/astral"
	"github.com/astralp2p/astral-go/astral/channel"
	"github.com/astralp2p/astral-go/lib/routing"
)

type opNewWatchArgs struct {
	Path  string `query:"required"`
	Name  string `query:"required"`
	Label string
	In    string
	Out   string
}

// OpNewWatch authorizes the caller under AdminObjects, then registers a new watched
// repository at the given path, starts an async background scan, and adds the
// repository to both the objects store and the local group.
func (mod *Module) OpNewWatch(ctx *astral.Context, q *routing.IncomingQuery, args opNewWatchArgs) (err error) {
	if !mod.Auth.Authorize(ctx, &auth.AdminObjectsAction{
		Action: auth.NewAction(q.Caller()),
		Repo:   astral.String8(args.Name),
		Path:   astral.String8(args.Path),
	}) {
		return q.Reject()
	}

	ch := q.Accept(channel.WithFormats(args.In, args.Out))
	defer ch.Close()

	// why: reported over the channel rather than as a rejection — the caller holds a
	// grant, so it gets the reason its path was refused
	path, err := validPath(args.Path)
	if err != nil {
		return ch.Send(astral.Err(err))
	}

	if args.Label == "" {
		args.Label = args.Name
	}

	repo, err := NewWatchRepository(mod, path, args.Label)
	if err != nil {
		return ch.Send(astral.Err(err))
	}

	scanCtx, cancel := mod.ctx.WithCancel()
	repo.scanCancel = cancel

	go func() {
		if err := mod.indexer.scan(scanCtx, path, true); err != nil {
			mod.log.Error("scan %v: %v", path, err)
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
