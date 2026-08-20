package user

import (
	"github.com/astralp2p/astral-go/astral"
	"github.com/astralp2p/astral-go/astral/channel"
	"github.com/astralp2p/astral-go/lib/routing"
)

type opSyncWithArgs struct {
	Node *astral.Identity `query:"required"`
	// fixme: Start is accepted and ignored; syncAssets reads the height from the
	// tree cursor. The spec documents it as the height to start from.
	Start astral.Uint64
	Out   string
}

// OpSyncWith authorizes the caller under AdminSwarm, then triggers an outbound
// asset sync with the specified node over the network zone.
//
// why AdminSwarm rather than SeeSwarm: the sync applies the remote log to this
// node's asset list, so the node named here decides what this node carries.
// That is the authority user.add_asset and user.remove_asset name.
func (mod *Module) OpSyncWith(ctx *astral.Context, q *routing.IncomingQuery, args opSyncWithArgs) (err error) {
	if !mod.authorizeAdminSwarm(ctx, q, args.Node, nil) {
		return q.RejectWithCode(4)
	}

	ch := channel.New(q.AcceptRaw(), channel.WithOutputFormat(args.Out))
	defer ch.Close()

	err = mod.syncAssets(ctx.IncludeZone(astral.ZoneNetwork), args.Node)
	if err != nil {
		return ch.Send(astral.NewError(err.Error()))
	}

	return ch.Send(&astral.Ack{})
}
