package user

import (
	"errors"

	"github.com/astralp2p/astral-go/api/auth"
	"github.com/astralp2p/astral-go/api/user"
	"github.com/astralp2p/astral-go/astral"
	"github.com/astralp2p/astral-go/astral/channel"
	"github.com/astralp2p/astral-go/lib/routing"
)

type opRemoveAssetArgs struct {
	ID  *astral.ObjectID `query:"required"`
	Out string
}

// OpRemoveAsset authorizes the caller under AdminSwarm, then removes an asset by
// object ID.
// Answers the error if removal fails.
func (mod *Module) OpRemoveAsset(ctx *astral.Context, q *routing.IncomingQuery, args opRemoveAssetArgs) (err error) {
	ch := channel.New(q.AcceptRaw(), channel.WithOutputFormat(args.Out))
	defer ch.Close()

	if !mod.Auth.Authorize(ctx, &user.AdminSwarmAction{
		Action:   auth.NewAction(q.Caller()),
		ObjectID: args.ID,
	}) {
		return ch.Send(auth.ErrNotPermitted)
	}

	err = mod.RemoveAsset(args.ID)
	switch {
	// why: a partial ID is the caller's mistake, so it gets its own code and is not logged as a node error.
	case errors.Is(err, astral.ErrPartialObjectID):
		return ch.Send(astral.ErrPartialObjectID)
	case err != nil:
		mod.log.Errorv(1, "remove asset: %v", err)
		return ch.Send(astral.Err(err))
	}

	return ch.Send(&astral.Ack{})
}
