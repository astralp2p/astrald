package fs

import (
	"github.com/astralp2p/astral-go/api/auth"
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
	// Temporary keeps the repository out of the tree, so it is gone after a restart.
	Temporary bool
	In        string
	Out       string
}

// OpNewRepo authorizes the caller under AdminObjects, then registers a new writable
// repository at the given path and adds it to the local group. Unless Temporary is set, the
// repository is saved to the tree and restored at the next start.
func (mod *Module) OpNewRepo(ctx *astral.Context, q *routing.IncomingQuery, args opNewRepoArgs) (err error) {
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

	var repo objectsmod.Repository

	persisted := NewRepository(mod, args.Name, path)
	persisted.persisted = !args.Temporary
	repo = persisted

	err = mod.Objects.AddRepository(args.Name, repo)
	if err != nil {
		return ch.Send(astral.Err(err))
	}

	err = mod.Objects.AddGroup(objects.RepoLocal, args.Name)
	if err != nil {
		return ch.Send(astral.Err(err))
	}

	if !args.Temporary {
		err = mod.persistAdded(ctx, args.Name, RepoConfig{Label: args.Label, Path: path, Writable: true})
		if err != nil {
			return ch.Send(astral.Err(err))
		}
	}

	return ch.Send(&astral.Ack{})
}
