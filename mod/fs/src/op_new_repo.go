package fs

import (
	"github.com/astralp2p/astral-go/api/objects"
	"github.com/astralp2p/astral-go/astral"
	"github.com/astralp2p/astral-go/astral/channel"
	"github.com/astralp2p/astral-go/lib/routing"
	objectsmod "github.com/astralp2p/astrald/mod/objects"
)

type opNewRepoArgs struct {
	Path  string `query:"required"`
	Name  string `query:"required"`
	Label string
	In    string
	Out   string
}

// OpNewRepo authorizes the caller under AdminObjects, then registers a new writable
// repository at the given path and adds it to the local group.
func (mod *Module) OpNewRepo(ctx *astral.Context, q *routing.IncomingQuery, args opNewRepoArgs) (err error) {
	if !mod.authorizeAdminObjects(ctx, q, args.Name, args.Path) {
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

	var repo objectsmod.Repository

	repo = NewRepository(mod, args.Name, path)

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
