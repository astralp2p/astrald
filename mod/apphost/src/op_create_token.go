package apphost

import (
	"time"

	"github.com/astralp2p/astral-go/astral"
	"github.com/astralp2p/astral-go/astral/channel"
	"github.com/astralp2p/astral-go/lib/routing"
)

const DefaultTokenDuration = astral.Duration(time.Hour * 24 * 365) // 1 year

type opCreateTokenArgs struct {
	Identity string `query:"required"`
	Duration astral.Duration
	Out      string
}

func (mod *Module) OpCreateToken(ctx *astral.Context, q *routing.IncomingQuery, args opCreateTokenArgs) (err error) {
	if q.Origin() == astral.OriginNetwork {
		return q.Reject()
	}

	if !mod.authorizeAdminManageApps(ctx, q) {
		return q.Reject()
	}

	ch := channel.New(q.AcceptRaw(), channel.WithOutputFormat(args.Out))
	defer ch.Close()

	identity, err := mod.Dir.ResolveIdentity(args.Identity)
	if err != nil {
		return ch.Send(astral.Err(err))
	}

	if identity.IsZero() {
		return ch.Send(astral.NewError("missing identity"))
	}

	if args.Duration == 0 {
		args.Duration = DefaultTokenDuration
	}

	mod.log.Logv(1, "creating token for %v valid for %v", identity, args.Duration)

	token, err := mod.CreateAccessToken(identity, args.Duration)
	if err != nil {
		mod.log.Errorv(1, "error creating token for %v: %v", identity, err)
		return ch.Send(astral.Err(err))
	}

	return ch.Send(token)
}
