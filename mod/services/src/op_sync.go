package services

import (
	"github.com/cryptopunkscc/astral-go/astral"
	"github.com/cryptopunkscc/astral-go/astral/channel"
	"github.com/cryptopunkscc/astrald/lib/routing"
)

type opSyncArgs struct {
	ID     string
	Follow bool   `query:"optional"`
	In     string `query:"optional"`
	Out    string `query:"optional"`
}

// OpSync fetches and caches services for the requested identity over ZoneNetwork.
// Any inbound channel message cancels the sync, allowing the caller to abort early.
func (mod *Module) OpSync(ctx *astral.Context, q *routing.IncomingQuery, args opSyncArgs) (err error) {
	ch := q.Accept(channel.WithFormats(args.In, args.Out))
	defer ch.Close()

	// resolve the target identity
	targetID, err := mod.Dir.ResolveIdentity(args.ID)
	if err != nil {
		return ch.Send(astral.NewError(err.Error()))
	}

	ctx, cancel := ctx.WithZone(astral.ZoneNetwork).WithCancel()

	// cancel the sync on any channel activity
	go func() {
		_, _ = ch.Receive()
		cancel()
	}()

	// run the updater
	err = mod.syncServices(ctx, targetID, args.Follow)
	if err != nil {
		return ch.Send(astral.NewError(err.Error()))
	}

	return ch.Send(&astral.Ack{})
}
