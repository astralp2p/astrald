package user

import (
	"github.com/cryptopunkscc/astral-go/api/auth"
	"github.com/cryptopunkscc/astral-go/astral"
	"github.com/cryptopunkscc/astral-go/astral/channel"
	"github.com/cryptopunkscc/astrald/lib/routing"
	"github.com/cryptopunkscc/astrald/mod/user"
)

type opInfoArgs struct {
	Out string `query:"optional"`
}

// OpInfo returns identity and contract metadata for this node's active contract.
// Requires an active contract (code 2 otherwise - the setup-mode probe); the
// caller must be authorized for user.InfoAction (code 4 otherwise) - the user
// and swarm members always are, other identities via authorizers.
func (mod *Module) OpInfo(ctx *astral.Context, q *routing.IncomingQuery, args opInfoArgs) (err error) {
	ac := mod.ActiveContract()
	if ac == nil {
		return q.RejectWithCode(2)
	}

	info := &user.InfoAction{Action: auth.NewAction(q.Caller())}
	if !mod.Auth.Authorize(ctx, info) {
		return q.RejectWithCode(4)
	}

	ch := q.Accept(channel.WithOutputFormat(args.Out))
	defer ch.Close()

	contractID, _ := astral.ResolveObjectID(ac)

	return ch.Send(&user.Info{
		NodeAlias:  astral.String8(mod.Dir.DisplayName(ac.Subject)),
		UserAlias:  astral.String8(mod.Dir.DisplayName(ac.Issuer)),
		ContractID: contractID,
		Contract:   ac,
	})
}
