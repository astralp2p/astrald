package apphost

import (
	"github.com/astralp2p/astral-go/astral"
	"github.com/astralp2p/astral-go/astral/channel"
	"github.com/astralp2p/astral-go/lib/routing"
	"github.com/astralp2p/astrald/mod/apphost"
)

type opNewAppContractArgs struct {
	ID       *astral.Identity
	Duration astral.Duration `query:"optional"`
	Out      string          `query:"optional"`
}

// OpNewAppContract creates an unsigned app contract for the given identity.
// The contract is returned to the caller for signing; use OpSignAppContract or OpInstallApp to complete the flow.
func (mod *Module) OpNewAppContract(ctx *astral.Context, q *routing.IncomingQuery, args opNewAppContractArgs) error {
	ch := channel.New(q.AcceptRaw(), channel.WithOutputFormat(args.Out))
	defer ch.Close()

	if args.Duration == 0 {
		args.Duration = DefaultAppContractDuration
	}

	contract, err := apphost.NewAppContract(args.ID, mod.node.Identity(), args.Duration.Duration())
	if err != nil {
		return ch.Send(astral.Err(err))
	}

	return ch.Send(contract)
}
