package user

import (
	"errors"

	"github.com/astralp2p/astral-go/api/auth"
	"github.com/astralp2p/astral-go/api/user"
	"github.com/astralp2p/astral-go/astral"
	"github.com/astralp2p/astral-go/astral/channel"
	"github.com/astralp2p/astral-go/lib/routing"
	"github.com/astralp2p/astrald/mod/objects"
)

type opRemoveAssetArgs struct {
	ID  *astral.ObjectID `query:"required"`
	Out string
}

// OpRemoveAsset authorizes the caller under AdminSwarm, then removes an asset by
// object ID.
// Rejects the query with an internal error code if removal fails, before the channel is accepted.
func (mod *Module) OpRemoveAsset(ctx *astral.Context, q *routing.IncomingQuery, args opRemoveAssetArgs) (err error) {
	if !mod.Auth.Authorize(ctx, &user.AdminSwarmAction{
		Action:   auth.NewAction(q.Caller()),
		ObjectID: args.ID,
	}) {
		return q.RejectWithCode(4)
	}

	err = mod.RemoveAsset(args.ID)
	switch {
	// why: a partial ID is the caller's mistake, so it gets its own code and is not logged as a node error.
	case errors.Is(err, objects.ErrPartialObjectID):
		return q.RejectWithCode(astral.CodeInvalidQuery)
	case err != nil:
		mod.log.Errorv(1, "remove asset: %v", err)
		return q.RejectWithCode(astral.CodeInternalError)
	}

	ch := channel.New(q.AcceptRaw(), channel.WithOutputFormat(args.Out))
	defer ch.Close()

	return ch.Send(&astral.Ack{})
}
