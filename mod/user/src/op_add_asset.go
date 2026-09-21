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

type opAddAssetArgs struct {
	ID  *astral.ObjectID `query:"required"`
	Out string
}

// OpAddAsset authorizes the caller under AdminSwarm, then adds the object to the
// user's asset list.
func (mod *Module) OpAddAsset(ctx *astral.Context, q *routing.IncomingQuery, args opAddAssetArgs) (err error) {
	if !mod.Auth.Authorize(ctx, &user.AdminSwarmAction{
		Action:   auth.NewAction(q.Caller()),
		ObjectID: args.ID,
	}) {
		return q.RejectWithCode(4)
	}

	err = mod.AddAsset(args.ID)
	switch {
	// why: a partial ID is the caller's mistake, so it gets its own code and is not logged as a node error.
	case errors.Is(err, objects.ErrPartialObjectID):
		return q.RejectWithCode(astral.CodeInvalidQuery)
	case err != nil:
		mod.log.Error("error adding asset: %v", err)
		return q.RejectWithCode(astral.CodeInternalError)
	}

	ch := channel.New(q.AcceptRaw(), channel.WithOutputFormat(args.Out))
	defer ch.Close()

	return ch.Send(&astral.Ack{})
}
