package apphost

import (
	"github.com/astralp2p/astral-go/api/auth"
	"github.com/astralp2p/astral-go/astral"
	"github.com/astralp2p/astral-go/astral/channel"
	"github.com/astralp2p/astral-go/lib/routing"
	apphostmod "github.com/astralp2p/astrald/mod/apphost"
)

type opCreateTokenArgs struct {
	Identity string `query:"required"`
	Duration astral.Duration
	Out      string
}

func (mod *Module) OpCreateToken(ctx *astral.Context, q *routing.IncomingQuery, args opCreateTokenArgs) (err error) {
	ch := channel.New(q.AcceptRaw(), channel.WithOutputFormat(args.Out))
	defer ch.Close()

	if q.Origin() == astral.OriginNetwork {
		return ch.Send(astral.NewErrOriginNotAllowed(q.Origin()))
	}

	if !mod.Auth.Authorize(ctx, &auth.AdminManageAppsAction{Action: auth.NewAction(q.Caller())}) {
		return ch.Send(auth.ErrNotPermitted)
	}

	identity, err := mod.Dir.ResolveIdentity(args.Identity)
	if err != nil {
		return ch.Send(astral.Err(err))
	}

	if identity.IsZero() {
		return ch.Send(astral.NewError("missing identity"))
	}

	if args.Duration == 0 {
		args.Duration = apphostmod.DefaultTokenDuration
	}

	mod.log.Logv(1, "creating token for %v valid for %v", identity, args.Duration)

	token, err := mod.CreateAccessToken(identity, args.Duration)
	if err != nil {
		mod.log.Errorv(1, "error creating token for %v: %v", identity, err)
		return ch.Send(astral.Err(err))
	}

	return ch.Send(token)
}
