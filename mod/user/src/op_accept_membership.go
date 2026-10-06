package user

import (
	"time"

	"github.com/astralp2p/astral-go/api/auth"
	"github.com/astralp2p/astral-go/api/crypto"
	"github.com/astralp2p/astral-go/api/user"
	"github.com/astralp2p/astral-go/astral"
	"github.com/astralp2p/astral-go/astral/channel"
	"github.com/astralp2p/astral-go/lib/routing"
	usermod "github.com/astralp2p/astrald/mod/user"
)

type opAcceptMembershipArgs struct {
	In  string
	Out string
}

// OpAcceptMembership handles the node side of the contract signing ceremony.
// Rejects if an active contract already exists (code 2).
// Reads the contract and the issuer's signature, validates the contract's
// subject and minimum remaining validity, verifies the issuer signature, and
// only then applies the invite policy.
// Self-refuses with user.ErrExpelled if this node holds the issuer's ban on itself.
// On success, stores the signed contract and sets it as the active contract.
func (mod *Module) OpAcceptMembership(ctx *astral.Context, q *routing.IncomingQuery, args opAcceptMembershipArgs) (err error) {
	ac := mod.ActiveContract()
	if ac != nil {
		// We already have an active contract
		return q.RejectWithCode(2)
	}

	conn := q.AcceptRaw()
	ch := channel.New(conn, channel.WithFormats(args.In, args.Out))
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
	case contract.ExpiresAt.Time().Before(time.Now().Add(usermod.MinimalContractLength)):
		return ch.Send(astral.Err(auth.ErrInvalidContract))
	}

	// why: self-refuse re-entry if this node already holds the issuer's ban on
	// itself — symmetry with IssueMembership, so a buggy or hostile issuer cannot
	// re-seat an expelled node. Effective only once the ban has propagated here.
	if mod.isExpelled(contract.Issuer, contract.Subject) {
		return ch.Send(user.ErrExpelled)
	}

	// why the signature is read and verified before the policy: the policy then
	// decides on a verified issuer, not a claimed one. Every client sends the
	// contract and the signature back to back before reading an answer
	// (astral-go api/user/client/accept_membership.go), so the read does not
	// wait on the decision.
	var issuerSig *crypto.Signature
	err = ch.Switch(channel.Expect(&issuerSig))
	if err != nil {
		return ch.Send(astral.Err(err))
	}

	signed := &auth.SignedContract{Contract: contract, IssuerSig: issuerSig}
	if err = mod.Auth.VerifyIssuer(signed); err != nil {
		return ch.Send(astral.Err(err))
	}

	// why only when delegated: the delegate may wait on the user, and only the
	// conn tells it that nobody waits for the signature any more. The inviter
	// sends nothing after the signature, so the watch reads nothing the op
	// needs. The accept-all policy answers at once, so without a delegate the
	// op serves an inviter that half-closes, as it did before delegation.
	policyCtx := ctx
	if delegated(&mod.config.SwarmInviteDelegate) {
		var stop func()
		policyCtx, stop = watchRequester(ctx, conn)
		defer stop()
	}

	approved := mod.GetSwarmInvitePolicy()(policyCtx, q.Caller(), contract)
	if !approved {
		return ch.Send(user.ErrInvitationDeclined)
	}

	// why: an approval that arrives after the inviter left would install a
	// contract whose issuer never receives the subject signature.
	if policyCtx.Err() != nil {
		mod.log.Logv(1, "invitation from %v approved after the inviter left; not joining", q.Caller())
		return nil
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
