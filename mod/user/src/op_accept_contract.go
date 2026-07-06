package user

import (
	"github.com/cryptopunkscc/astrald/astral"
	"github.com/cryptopunkscc/astrald/astral/channel"
	"github.com/cryptopunkscc/astrald/lib/routing"
	"github.com/cryptopunkscc/astrald/mod/auth"
)

type opAcceptContractArgs struct {
	In  string `query:"optional"`
	Out string `query:"optional"`
}

// OpAcceptContract activates a fully-signed node contract as the node's active
// contract - the local-setup and cold-card counterpart of OpAcceptMembership,
// which runs the signing handshake instead of taking a pre-signed contract.
// Rejects if the node already has an active contract (code 2). The contract is
// validated (signatures, subject == node, not expired, grants membership)
// before anything is stored or activated; the active-contract store's follow
// indexes it for delegation lookups.
func (mod *Module) OpAcceptContract(ctx *astral.Context, q *routing.IncomingQuery, args opAcceptContractArgs) (err error) {
	ac := mod.ActiveContract()
	if ac != nil {
		// the node already belongs to a user - claiming is a one-time transition
		return q.RejectWithCode(2)
	}

	ch := q.Accept(channel.WithFormats(args.In, args.Out))
	defer ch.Close()

	var signed *auth.SignedContract
	err = ch.Switch(channel.Expect(&signed))
	if err != nil {
		return ch.Send(astral.Err(err))
	}

	// gate before any side-effect, so an invalid contract touches nothing
	err = mod.validateActiveContract(signed)
	if err != nil {
		return ch.Send(astral.Err(err))
	}

	_, err = mod.Objects.Store(ctx, mod.Objects.WriteDefault(), signed)
	if err != nil {
		return ch.Send(astral.Err(err))
	}

	err = mod.SetActiveContract(signed)
	if err != nil {
		return ch.Send(astral.NewError(err.Error()))
	}

	return ch.Send(&astral.Ack{})
}
