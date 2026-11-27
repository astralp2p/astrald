package user

import (
	"errors"
	"time"

	"github.com/astralp2p/astral-go/api/auth"
	"github.com/astralp2p/astral-go/api/secp256k1"
	"github.com/astralp2p/astral-go/api/tree"
	"github.com/astralp2p/astral-go/api/user"
	"github.com/astralp2p/astral-go/astral"
	cryptomod "github.com/astralp2p/astrald/mod/crypto"
)

// The faults of a membership a carrier hands over.
var (
	errHandedNoMembership    = errors.New("carrier handed over no membership")
	errHandedOtherSubject    = errors.New("carrier handed over another node's membership")
	errHandedOtherTier       = errors.New("carrier handed over a membership at another tier")
	errHandedOtherRequest    = errors.New("carrier handed over a membership for another request")
	errHandedOtherIssuer     = errors.New("carrier handed over a membership another node issued")
	errHandedOtherMembership = errors.New("carrier confirmed another membership than this node signed")
)

// swarmJoiner is the node that asks to join. It keeps one signed JoinRequest,
// in pending, until it has joined, been declined, or seen it expire, and
// resends that same object, with the same entry ID, on every reconnect.
type swarmJoiner struct {
	self   *astral.Identity
	signer cryptomod.Module
	// pending is config.PendingJoin: the request survives a restart.
	pending *tree.Value[*user.SignedJoinRequest]
	ctx     *astral.Context // of the writes to pending
	now     func() time.Time
}

func newSwarmJoiner(ctx *astral.Context, self *astral.Identity, signer cryptomod.Module, pending *tree.Value[*user.SignedJoinRequest]) *swarmJoiner {
	return &swarmJoiner{self: self, signer: signer, pending: pending, ctx: ctx, now: time.Now}
}

// Request returns the kept request when it names carrier and tier and has not
// expired, and otherwise signs and keeps a new one.
func (j *swarmJoiner) Request(ctx *astral.Context, carrier *astral.Identity, tier user.Tier) (*user.SignedJoinRequest, error) {
	kept := j.pending.Get()
	if !kept.IsNil() && kept.Carrier.IsEqual(carrier) && kept.Tier == tier && !kept.Expired(j.now()) {
		return kept, nil
	}

	now := astral.Time(j.now())
	return j.sign(ctx, &user.JoinRequest{
		Joiner:  j.self,
		Carrier: carrier,
		Tier:    tier,
		FiledAt: now,
		Timeout: user.RequestDeadline(now),
	})
}

// newInvite returns the pre-filled JoinRequest by which carrier invites
// joiner at tier. The joiner signs it unchanged (Accept).
func newInvite(carrier, joiner *astral.Identity, tier user.Tier, now time.Time) *user.JoinRequest {
	filed := astral.Time(now)
	return &user.JoinRequest{
		Joiner:  joiner,
		Carrier: carrier,
		Tier:    tier,
		FiledAt: filed,
		Timeout: user.RequestDeadline(filed),
	}
}

// Accept signs and keeps an invite, a JoinRequest the carrier pre-filled for
// this node.
func (j *swarmJoiner) Accept(ctx *astral.Context, invite *user.JoinRequest) (*user.SignedJoinRequest, error) {
	switch {
	case invite == nil || !invite.Joiner.IsEqual(j.self):
		return nil, errors.New("invite names another joiner")
	case !invite.Tier.IsValid():
		return nil, errors.New("invite names an invalid tier")
	case !invite.ValidTimeout() || invite.Expired(j.now()):
		return nil, errJoinExpired
	}
	return j.sign(ctx, invite)
}

// sign signs req as this node and keeps it in pending.
func (j *swarmJoiner) sign(ctx *astral.Context, req *user.JoinRequest) (*user.SignedJoinRequest, error) {
	sig, err := j.signer.Sign(ctx, secp256k1.FromIdentity(j.self), req)
	if err != nil {
		return nil, err
	}

	signed := &user.SignedJoinRequest{JoinRequest: req, JoinerSig: sig}
	return signed, j.pending.Set(j.ctx, signed)
}

// Join sends req to its carrier through carry, co-signs the membership the
// carrier answers, and returns the membership the carrier confirms. A
// membership, a Decline, an expiry or a carrier that refuses the request or
// its tier ends the join and drops req; any other error keeps req for the
// next Join.
//
// carry is the connection to the carrier req names: it sends req, has cosign
// sign the membership the carrier answers, and returns the membership the
// carrier confirms with both signatures, or the end of the join.
func (j *swarmJoiner) Join(ctx *astral.Context, req *user.SignedJoinRequest, carry func(ctx *astral.Context, req *user.SignedJoinRequest, cosign joinCosign) (*auth.SignedContract, error)) (*auth.SignedContract, error) {
	if req.Expired(j.now()) {
		return nil, errors.Join(errJoinExpired, j.pending.Clear(j.ctx))
	}

	var signed *auth.SignedContract
	cosign := func(ctx *astral.Context, half *auth.SignedContract) (*auth.SignedContract, error) {
		sc, err := j.cosign(ctx, req, half)
		signed = sc
		return sc, err
	}

	var end joinEnd
	sc, err := carry(ctx, req, cosign)
	switch {
	case err == nil:
		if err = j.checkConfirmed(req, signed, sc); err != nil {
			return nil, err
		}
		return sc, j.pending.Clear(j.ctx)
	case errors.As(err, &end) && end.closesRequest():
		return nil, errors.Join(err, j.pending.Clear(j.ctx))
	}
	return nil, err
}

// cosign checks half, the membership the carrier answered, against req, and
// signs it as this node, the subject.
func (j *swarmJoiner) cosign(ctx *astral.Context, req *user.SignedJoinRequest, half *auth.SignedContract) (*auth.SignedContract, error) {
	if err := j.checkMembership(req, half); err != nil {
		return nil, err
	}
	sig, err := j.signer.Sign(ctx, secp256k1.FromIdentity(j.self), half.Contract)
	if err != nil {
		return nil, err
	}
	return &auth.SignedContract{Contract: half.Contract, IssuerSig: half.IssuerSig, SubjectSig: sig}, nil
}

// checkMembership checks that sc admits this node at the requested tier
// alone, answers req, is issued by req's carrier (the user key at
// privileged_member) and has not expired.
//
// note: the signatures and the issuer's tier are checked when sc enters
// SwarmState (the entry check) and when it is read (SwarmView.Membership).
func (j *swarmJoiner) checkMembership(req *user.SignedJoinRequest, sc *auth.SignedContract) error {
	reqID, err := astral.ResolveObjectID(req)
	if err != nil {
		return err
	}
	m, ok := user.MembershipOf(sc)
	switch {
	case !ok || sc.IssuerSig == nil:
		return errHandedNoMembership
	case !m.Subject.IsEqual(j.self):
		return errHandedOtherSubject
	case m.Tier != req.Tier || !user.TierOnly(sc.Contract, req.Tier):
		return errHandedOtherTier
	case !reqID.IsEqual(sc.RequestID):
		return errHandedOtherRequest
	case req.Tier != user.TierPrivilegedMember && !sc.Issuer.IsEqual(req.Carrier):
		return errHandedOtherIssuer
	case !j.now().Before(sc.ExpiresAt.Time()):
		return auth.ErrContractExpired
	}
	return nil
}

// checkConfirmed checks that sc, the membership the carrier confirmed, is the
// one this node signed, with both signatures.
func (j *swarmJoiner) checkConfirmed(req *user.SignedJoinRequest, signed, sc *auth.SignedContract) error {
	if signed.IsNil() || sc.IsNil() || sc.SubjectSig == nil || !sameBody(signed.Contract, sc.Contract) {
		return errHandedOtherMembership
	}
	return j.checkMembership(req, sc)
}
