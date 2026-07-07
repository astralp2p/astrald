package user

import (
	"github.com/cryptopunkscc/astral-go/astral"
	"github.com/cryptopunkscc/astral-go/astral/channel"
	"github.com/cryptopunkscc/astrald/lib/routing"
)

type opAddAssetArgs struct {
	ID  *astral.ObjectID
	Out string `query:"optional"`
}

func (mod *Module) OpAddAsset(ctx *astral.Context, q *routing.IncomingQuery, args opAddAssetArgs) (err error) {
	err = mod.AddAsset(args.ID)
	if err != nil {
		mod.log.Error("error adding asset: %v", err)
		return q.RejectWithCode(astral.CodeInternalError)
	}

	ch := channel.New(q.AcceptRaw(), channel.WithOutputFormat(args.Out))
	defer ch.Close()

	return ch.Send(&astral.Ack{})
}
