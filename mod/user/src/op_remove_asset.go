package user

import (
	"github.com/cryptopunkscc/astral-go/astral"
	"github.com/cryptopunkscc/astral-go/astral/channel"
	"github.com/cryptopunkscc/astral-go/lib/routing"
)

type opRemoveAssetArgs struct {
	ID  *astral.ObjectID
	Out string `query:"optional"`
}

// OpRemoveAsset removes an asset by object ID.
// Rejects the query with an internal error code if removal fails, before the channel is accepted.
func (mod *Module) OpRemoveAsset(ctx *astral.Context, q *routing.IncomingQuery, args opRemoveAssetArgs) (err error) {
	err = mod.RemoveAsset(args.ID)

	if err != nil {
		mod.log.Errorv(1, "remove asset: %v", err)
		return q.RejectWithCode(astral.CodeInternalError)
	}

	ch := channel.New(q.AcceptRaw(), channel.WithOutputFormat(args.Out))
	defer ch.Close()

	return ch.Send(&astral.Ack{})
}
