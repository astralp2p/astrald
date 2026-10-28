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
// Refuses with user.ErrAlreadyClaimed if an active contract already exists.
// Validates contract subject, identity match, issuer distinct from subject,
// minimum remaining validity and the swarm membership permit, then verifies the issuer's signature before applying the invite policy.
// Self-refuses with user.ErrExpelled if this node holds the issuer's ban on itself.
// On success, stores the signed contract and sets it as the active contract.
func (mod *Module) OpAcceptMembership(ctx *astral.Context, q *routing.IncomingQuery, args opAcceptMembershipArgs) (err error) {
	ac := mod.ActiveContract()
	if ac != nil {
		// We already have an active contract
		ch := q.Accept(channel.WithFormats(args.In, args.Out))
		defer ch.Close()
		return ch.Send(user.ErrAlreadyClaimed)
	}

	conn := q.AcceptRaw()
	ch := channel.New(conn, channel.WithFormats(args.In, args.Out))
	defer ch.Close()

	// receive the contract to sign
	var contract *auth.Contract
	err = ch.Switch(channel.Expect(&contract), channel.FailOnEOF())
	if err != nil {
		return ch.Send(astral.Err(err))
	}

	// check contract viability
	switch {
	case contract.Subject.IsZero():
		return ch.Send(astral.Err(auth.ErrInvalidContract))
	case !contract.Subject.IsEqual(mod.node.Identity()):
		return ch.Send(astral.Err(auth.ErrInvalidContract))
	case contract.Issuer.IsEqual(contract.Subject):
		return ch.Send(astral.Err(auth.ErrInvalidContract))
	case !user.IsNodeContract(contract):
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

	// why the signature before the policy: the client sends contract and
	// signature back to back, the watch below consumes the pipe, and a delegate
	// decides on an invitation whose issuer is verified.
	var issuerSig *crypto.Signature
	err = ch.Switch(channel.Expect(&issuerSig))
	if err != nil {
		return ch.Send(astral.Err(err))
	}

	signed := &auth.SignedContract{Contract: contract, IssuerSig: issuerSig}
	if err = mod.Auth.VerifyIssuer(signed); err != nil {
		return ch.Send(astral.Err(err))
	}

	// why: the inviter sends nothing more, and only the conn tells a waiting
	// decision that nobody waits for its answer.
	policyCtx, stop := routing.UntilDisconnect(ctx, conn)
	defer stop()

	dec, err := mod.GetSwarmInvitePolicy()(policyCtx, &user.SwarmInviteRequest{Inviter: q.Caller(), Contract: contract})
	switch {
	case err != nil:
		mod.log.Logv(1, "invitation from %v declined: %v", q.Caller(), err)
		return ch.Send(user.ErrInvitationDeclined)
	case !bool(dec.Allow):
		return ch.Send(user.ErrInvitationDeclined)
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
