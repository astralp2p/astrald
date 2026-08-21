package objects

import (
	"github.com/astralp2p/astral-go/astral"
	"github.com/astralp2p/astral-go/astral/channel"
	"github.com/astralp2p/astral-go/lib/routing"
)

type opPurgeArgs struct {
	Repo string `query:"required"`
	Out  string
	Zone *astral.Zone
}

// OpPurge deletes unheld objects from a repository, streaming each purged ObjectID
// then a final error or EOS. Defaults to ZoneAll when no zone is given.
func (mod *Module) OpPurge(ctx *astral.Context, q *routing.IncomingQuery, args opPurgeArgs) error {
	if !mod.authorizeAdminObjects(ctx, q, nil, args.Repo) {
		return q.Reject()
	}

	ctx = ctx.WithIdentity(q.Caller())
	if args.Zone == nil {
		ctx = ctx.WithZone(astral.ZoneAll)
	} else {
		ctx = ctx.WithZone(*args.Zone)
	}

	ch := channel.New(q.AcceptRaw(), channel.WithOutputFormat(args.Out))
	defer ch.Close()

	repo := mod.GetRepository(args.Repo)
	if repo == nil {
		return ch.Send(astral.NewError("repository not found"))
	}

	ctx, cancel := ctx.WithCancel()
	defer cancel()

	purged, errPtr := mod.purgeRepository(ctx, repo)
	for id := range purged {
		err := ch.Send(id)
		if err != nil {
			return err
		}
	}

	if *errPtr != nil {
		return ch.Send(astral.Err(*errPtr))
	}

	return ch.Send(&astral.EOS{})
}
