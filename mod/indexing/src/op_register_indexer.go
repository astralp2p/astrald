package indexing

import (
	"github.com/astralp2p/astral-go/api/auth"
	"github.com/astralp2p/astral-go/astral"
	"github.com/astralp2p/astral-go/astral/channel"
	"github.com/astralp2p/astral-go/lib/routing"
)

type opRegisterIndexerArgs struct {
	Name string `query:"required"`
	In   string
	Out  string
}

// OpRegisterIndexer registers the caller as the owner of a named indexer and
// returns its nonce. The caller must hold ServeObjects for the indexer role.
func (mod *Module) OpRegisterIndexer(ctx *astral.Context, q *routing.IncomingQuery, args opRegisterIndexerArgs) error {
	// why: cheapest refusal first. Indexing state is this node's own, so a network
	// caller is refused whatever it holds.
	if q.Origin() == astral.OriginNetwork {
		return q.Reject()
	}

	if !mod.Auth.Authorize(ctx, &auth.ServeObjectsAction{
		Action: auth.NewAction(q.Caller()),
		Role:   auth.RoleIndexer,
	}) {
		return q.Reject()
	}

	ch := q.Accept(channel.WithFormats(args.In, args.Out))
	defer ch.Close()

	nonce, err := mod.RegisterIndexer(ctx, q.Caller(), args.Name)
	if err != nil {
		return ch.Send(astral.Err(err))
	}

	return ch.Send(&nonce)
}
