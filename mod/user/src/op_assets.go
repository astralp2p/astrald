package user

import (
	"github.com/astralp2p/astral-go/astral"
	"github.com/astralp2p/astral-go/astral/channel"
	"github.com/astralp2p/astral-go/lib/routing"
)

type opAssetsArgs struct {
	Out string
}

// OpAssets authorizes the caller under SeeSwarm, then streams all module assets
// to the caller, terminating with EOS. On send failure it attempts to deliver an
// error frame before returning.
func (mod *Module) OpAssets(ctx *astral.Context, q *routing.IncomingQuery, args opAssetsArgs) (err error) {
	if !mod.authorizeSeeSwarm(ctx, q) {
		return q.RejectWithCode(4)
	}

	ch := channel.New(q.AcceptRaw(), channel.WithOutputFormat(args.Out))
	defer ch.Close()

	for _, asset := range mod.Assets() {
		err = ch.Send(asset)
		if err != nil {
			return ch.Send(astral.NewError(err.Error()))
		}
	}

	return ch.Send(&astral.EOS{})
}
