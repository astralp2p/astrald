package apphost

import (
	"github.com/cryptopunkscc/astral-go/astral"
	"github.com/cryptopunkscc/astral-go/astral/channel"
	"github.com/cryptopunkscc/astrald/lib/routing"
	"github.com/cryptopunkscc/astrald/mod/apphost"
	"github.com/cryptopunkscc/astrald/mod/auth"
)

type opInstallAppArgs struct {
	ID       *astral.Identity
	Duration astral.Duration `query:"optional"`
	Out      string          `query:"optional"`
}

// OpInstallApp creates, signs, and indexes an app contract for an existing identity, then records it as a local app.
// The node signs as both issuer and subject because it holds both keys at install time.
// On success the signed contract is pushed to the local user swarm.
func (mod *Module) OpInstallApp(ctx *astral.Context, q *routing.IncomingQuery, args opInstallAppArgs) error {
	if q.Origin() == astral.OriginNetwork {
		return q.RejectWithCode(3)
	}

	ch := channel.New(q.AcceptRaw(), channel.WithOutputFormat(args.Out))
	defer ch.Close()

	if args.Duration == 0 {
		args.Duration = DefaultAppContractDuration
	}

	contract, err := apphost.NewAppContract(args.ID, mod.node.Identity(), args.Duration.Duration())
	if err != nil {
		return ch.Send(astral.Err(err))
	}

	// Node holds both app and node keys — SignContract signs as both issuer and subject
	signed := &auth.SignedContract{Contract: contract}
	if err = mod.Auth.SignContract(ctx, signed); err != nil {
		return ch.Send(astral.Err(err))
	}

	if err = mod.Auth.IndexContract(ctx, signed); err != nil {
		return ch.Send(astral.Err(err))
	}

	if _, err = mod.Objects.Store(ctx, mod.Objects.WriteDefault(), signed); err != nil {
		return ch.Send(astral.Err(err))
	}

	if err = mod.db.CreateLocalApp(args.ID, mod.node.Identity()); err != nil {
		return ch.Send(astral.Err(err))
	}

	mod.log.Logv(1, "installed app %v", args.ID)
	go mod.User.PushToLocalSwarm(mod.ctx, signed)

	return ch.Send(signed)
}
