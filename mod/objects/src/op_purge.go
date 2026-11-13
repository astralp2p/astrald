package objects

import (
	"time"

	"github.com/astralp2p/astral-go/api/auth"
	"github.com/astralp2p/astral-go/astral"
	"github.com/astralp2p/astral-go/astral/channel"
	"github.com/astralp2p/astral-go/lib/routing"
)

type opPurgeArgs struct {
	Repo      string `query:"required"`
	Untouched *astral.Duration
	Dry       bool
	Out       string
	Zone      *astral.Zone
}

// OpPurge deletes the objects of a repository that nothing holds and that were not touched within Untouched,
// streaming each purged ObjectID then a final error or EOS. A group is purged once per leaf.
// Untouched below the configured floor (purge_floor, one hour by default), or none, means the floor.
// Dry answers the same ObjectIDs and deletes nothing.
// Defaults to ZoneAll when no zone is given.
func (mod *Module) OpPurge(ctx *astral.Context, q *routing.IncomingQuery, args opPurgeArgs) error {
	ch := channel.New(q.AcceptRaw(), channel.WithOutputFormat(args.Out))
	defer ch.Close()

	// why AdminObjects and not StoreObjects: a grant to add objects does not grant
	// destroying them.
	if !mod.Auth.Authorize(ctx, &auth.AdminObjectsAction{
		Action: auth.NewAction(q.Caller()),
		Repo:   astral.String8(args.Repo),
	}) {
		return ch.Send(auth.ErrNotPermitted)
	}

	ctx = ctx.WithIdentity(q.Caller())
	if args.Zone == nil {
		ctx = ctx.WithZone(astral.ZoneAll)
	} else {
		ctx = ctx.WithZone(*args.Zone)
	}

	repo := mod.GetRepository(args.Repo)
	if repo == nil {
		return ch.Send(astral.NewError("repository not found"))
	}

	ctx, cancel := ctx.WithCancel()
	defer cancel()

	var untouched time.Duration
	if args.Untouched != nil {
		untouched = args.Untouched.Duration()
	}

	purged, errPtr := mod.purgeRepository(ctx, repo, untouched, args.Dry)
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
