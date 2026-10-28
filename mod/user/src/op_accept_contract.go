package user

import (
	"github.com/astralp2p/astral-go/api/auth"
	"github.com/astralp2p/astral-go/api/user"
	"github.com/astralp2p/astral-go/astral"
	"github.com/astralp2p/astral-go/astral/channel"
	"github.com/astralp2p/astral-go/lib/routing"
)

type opAcceptContractArgs struct {
	In  string
	Out string
}

// OpAcceptContract activates a fully-signed node contract as the node's active
// contract - the local-setup and cold-card counterpart of OpAcceptMembership,
// which runs the signing handshake instead of taking a pre-signed contract.
// Refuses with user.ErrAlreadyClaimed if the node already has an active contract. The contract is
// validated (signatures, subject == node, not expired, grants membership)
// before anything is stored or activated; the active-contract store's follow
// indexes it for delegation lookups.
//
// why: no action gates this op, by decision. Claimed, the refusal below refuses
// every caller. Unclaimed, there is no active contract and so no permit any
// caller could hold — both swarm authorizers refuse on a nil contract, so an
// action here would refuse everyone in the only window the op acts in. The gate
// is the artifact: validateActiveContract requires a subject signature made by
// this node's own key. Revisit when the claim window admits an identity.
func (mod *Module) OpAcceptContract(ctx *astral.Context, q *routing.IncomingQuery, args opAcceptContractArgs) (err error) {
	ac := mod.ActiveContract()
	if ac != nil {
		// the node already belongs to a user - claiming is a one-time transition
		ch := q.Accept(channel.WithFormats(args.In, args.Out))
		defer ch.Close()
		return ch.Send(user.ErrAlreadyClaimed)
	}

	ch := q.Accept(channel.WithFormats(args.In, args.Out))
	defer ch.Close()

	var signed *auth.SignedContract
	err = ch.Switch(channel.Expect(&signed), channel.FailOnEOF())
	if err != nil {
		return ch.Send(astral.Err(err))
	}

	// gate before any side-effect, so an invalid contract touches nothing
	err = mod.validateActiveContract(signed)
	if err != nil {
		return ch.Send(astral.Err(err))
	}

	err = mod.claim(ctx, signed)
	if err != nil {
		return ch.Send(astral.Err(err))
	}

	return ch.Send(&astral.Ack{})
}
