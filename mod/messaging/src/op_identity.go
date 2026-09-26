package messaging

import (
	"errors"

	"github.com/astralp2p/astral-go/api/auth"
	"github.com/astralp2p/astral-go/api/messaging"
	"github.com/astralp2p/astral-go/astral"
	"github.com/astralp2p/astral-go/astral/channel"
	"github.com/astralp2p/astral-go/lib/routing"
	"gorm.io/gorm"
)

type opIdentityArgs struct {
	Identity string `query:"required"`
	Out      string
}

// OpIdentity answers one participant's record without its access token.
// Identity takes an identity or an alias. A participant is any identity the
// hosting index names, served or not.
//
// why this op answers no token: its guard is SeeNodeStateAction, which grants
// participant metadata and no credential, so the answer is IdentityInfo.
//
// why the caller is authorized as well as the origin refused: the origin
// refusal reads what the query carries, and apphost's endpoints stamp none. A
// participant's token authenticates there, so the origin refusal alone lets a
// participant read every tenant's record.
func (mod *Module) OpIdentity(ctx *astral.Context, q *routing.IncomingQuery, args opIdentityArgs) error {
	if refusesOrigin(q) {
		return q.Reject()
	}

	if !mod.Auth.Authorize(ctx, &auth.SeeNodeStateAction{Action: auth.NewAction(q.Caller())}) {
		return q.Reject()
	}

	ch := channel.New(q.AcceptRaw(), channel.WithOutputFormat(args.Out))
	defer ch.Close()

	identity, err := mod.Dir.ResolveIdentity(args.Identity)
	if err != nil {
		return ch.Send(astral.NewError("unknown identity"))
	}

	row, err := mod.db.FindMailbox(identity)
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return ch.Send(astral.NewError("identity not found"))
	}
	if err != nil {
		return ch.Send(astral.Err(err))
	}

	// note: an identity without an alias answers the empty one.
	alias, _ := mod.Dir.GetAlias(row.Identity)

	return ch.Send(&messaging.IdentityInfo{
		Identity: row.Identity,
		Alias:    astral.String8(alias),
	})
}
