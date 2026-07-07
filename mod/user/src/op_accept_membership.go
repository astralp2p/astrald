package user

import (
	"time"

	"github.com/cryptopunkscc/astral-go/api/auth"
	"github.com/cryptopunkscc/astral-go/api/crypto"
	"github.com/cryptopunkscc/astral-go/api/user"
	"github.com/cryptopunkscc/astral-go/astral"
	"github.com/cryptopunkscc/astral-go/astral/channel"
	"github.com/cryptopunkscc/astrald/lib/routing"
)

type opAcceptMembershipArgs struct {
	In  string `query:"optional"`
	Out string `query:"optional"`
}

// OpAcceptMembership handles the node side of the contract signing ceremony.
// Rejects if an active contract already exists (code 2).
// Validates contract subject, identity match, and minimum remaining validity before applying the invite policy.
// Self-refuses with user.ErrExpelled if this node holds the issuer's ban on itself.
// On success, stores the signed contract and sets it as the active contract.
func (mod *Module) OpAcceptMembership(ctx *astral.Context, q *routing.IncomingQuery, args opAcceptMembershipArgs) (err error) {
	ac := mod.ActiveContract()
	if ac != nil {
		// We already have an active contract
		return q.RejectWithCode(2)
	}

	ch := q.Accept(channel.WithFormats(args.In, args.Out))
	defer ch.Close()

	// receive the contract to sign
	var contract *auth.Contract
	err = ch.Switch(channel.Expect(&contract))
	if err != nil {
		return ch.Send(astral.Err(err))
	}

	// check contract viability
	switch {
	case contract.Subject.IsZero():
		return ch.Send(astral.Err(auth.ErrInvalidContract))
	case !contract.Subject.IsEqual(mod.node.Identity()):
		return ch.Send(astral.Err(auth.ErrInvalidContract))
	case contract.ExpiresAt.Time().Before(time.Now().Add(minimalContractLength)):
		return ch.Send(astral.Err(auth.ErrInvalidContract))
	}

	// why: self-refuse re-entry if this node already holds the issuer's ban on
	// itself — symmetry with IssueMembership, so a buggy or hostile issuer cannot
	// re-seat an expelled node. Effective only once the ban has propagated here.
	if mod.isExpelled(contract.Issuer, contract.Subject) {
		return ch.Send(user.ErrExpelled)
	}

	approved := mod.GetSwarmInvitePolicy()(q.Caller(), contract)
	if !approved {
		return ch.Send(user.ErrInvitationDeclined)
	}

	var issuerSig *crypto.Signature
	err = ch.Switch(channel.Expect(&issuerSig))
	if err != nil {
		return ch.Send(astral.Err(err))
	}

	signed := &auth.SignedContract{Contract: contract, IssuerSig: issuerSig}
	if err = mod.Auth.VerifyIssuer(signed); err != nil {
		return ch.Send(astral.Err(err))
	}

	subjectSig, err := mod.Auth.SignSubject(ctx, signed)
	if err != nil {
		return ch.Send(astral.Err(err))
	}

	err = ch.Send(subjectSig)
	if err != nil {
		return
	}

	err = mod.Auth.IndexContract(ctx, signed)
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

	return nil
}
