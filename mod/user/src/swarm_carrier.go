package user

import (
	"errors"
	"time"

	"github.com/astralp2p/astral-go/api/auth"
	"github.com/astralp2p/astral-go/api/crypto"
	"github.com/astralp2p/astral-go/api/secp256k1"
	"github.com/astralp2p/astral-go/api/user"
	"github.com/astralp2p/astral-go/astral"
	"github.com/astralp2p/astral-go/sig"
	usermod "github.com/astralp2p/astrald/mod/user"
	"github.com/astralp2p/astrald/mod/user/src/swarmset"
)

// The ends of a join a carrier reports to its joiner. Their text is part of
// the protocol (astral-docs protocols/user/ops/user.carry_join.md).
var (
	errJoinDeclined      = errors.New("join request declined")
	errJoinExpired       = errors.New("join request expired")
	errNotJoinCarrier    = errors.New("join request names another carrier")
	errCarrierTier       = errors.New("only a trusted or privileged member carries a join request")
	errTierAboveIssuer   = errors.New("tier above what this node may issue")
	errUserKeyRequired   = errors.New("a privileged membership needs the user key's signature")
	errNotSignedByJoiner = errors.New("membership is not signed by the joiner")
	errFiledLater        = errors.New("join request is filed later than this node's time")
)

// defaultSwarmPoll is how often a carrier reads SwarmState's head while it
// waits for the user's confirmation.
// todo: switch to vfs.watch when it lands.
const defaultSwarmPoll = 2 * time.Second

// entrySigner signs a SwarmState entry as key. crypto.Module.Sign is one: it
// signs the hash with a held key, or the BIP-137 text on a Coldcard.
type entrySigner interface {
	Sign(ctx *astral.Context, key *crypto.PublicKey, obj crypto.SignableTextObject) (*crypto.Signature, error)
}

// joinCosign is the joiner's side of a carrier session: it takes the
// membership its issuer signed and returns it signed by the joiner as
// subject.
type joinCosign func(ctx *astral.Context, half *auth.SignedContract) (*auth.SignedContract, error)

// swarmCarrier carries a joiner's JoinRequest into SwarmState and signs the
// joiner's membership with the joiner in the carrier session: the issuer
// signs, the joiner co-signs, and the carrier stores the contract with both
// signatures. It keeps nothing between connections: a joiner that reconnects
// resends the same request and the carrier answers a membership with the same
// body.
// Spec: .ai/system/protocols/user/README.md#joins
type swarmCarrier struct {
	self      *astral.Identity
	state     carrierState
	view      usermod.SwarmView
	signer    entrySigner
	ceremony  *userKeyCeremony   // nil: this node cannot reach the user key
	confirmed *joinConfirmations // what user.confirm_join signed on this node
	validity  time.Duration      // of the memberships it signs
	poll      time.Duration
	now       func() time.Time
}

// carrierState is the part of usermod.SwarmState a carrier uses.
type carrierState interface {
	SwarmHead() uint64
	FileJoinRequest(ctx *astral.Context, req *user.SignedJoinRequest) (*astral.ObjectID, error)
	AddContract(ctx *astral.Context, sc *auth.SignedContract) error
}

func newSwarmCarrier(self *astral.Identity, state carrierState, view usermod.SwarmView, signer entrySigner) *swarmCarrier {
	return &swarmCarrier{
		self:      self,
		state:     state,
		view:      view,
		signer:    signer,
		confirmed: &joinConfirmations{},
		validity:  usermod.DefaultContractValidity,
		poll:      defaultSwarmPoll,
		now:       time.Now,
	}
}

// withCeremony returns this carrier with ceremony as the user key it reaches.
//
// why per carry: the user key is stored on, or removed from, this node while
// it runs, so the ceremony is read when a join arrives.
func (c *swarmCarrier) withCeremony(ceremony *userKeyCeremony) *swarmCarrier {
	cc := *c
	cc.ceremony = ceremony
	return &cc
}

// Carry writes req into SwarmState and runs the carrier session: it has the
// membership signed by its issuer, has the joiner co-sign it through cosign,
// stores it with both signatures and returns it. A Decline, the request's
// Timeout or an expelled joiner ends the join with errJoinDeclined or
// errJoinExpired, and no membership is signed for it. An ended ctx (a dropped
// connection) only pauses it: the joiner resends req.
func (c *swarmCarrier) Carry(ctx *astral.Context, req *user.SignedJoinRequest, cosign joinCosign) (*auth.SignedContract, error) {
	if err := c.admit(req); err != nil {
		return nil, err
	}

	reqID, err := c.state.FileJoinRequest(ctx, req)
	if err != nil {
		return nil, err
	}
	if err = c.open(req, reqID); err != nil {
		return nil, err
	}

	stored := c.stored(req, reqID)
	half, err := c.issue(ctx, req, reqID, stored)
	if err != nil {
		return nil, err
	}
	signed, err := cosign(ctx, half)
	if err != nil {
		return nil, err
	}
	return c.complete(ctx, half, signed, stored)
}

// admit refuses a request this node may not carry: one not signed, naming
// another carrier or an invalid tier, filed later than this node's time, a
// carrier below Trusted (D5, D10), or a tier this node's tier may not issue
// (swarmset.MayIssue); the user key issues a privileged_member membership.
func (c *swarmCarrier) admit(req *user.SignedJoinRequest) error {
	switch {
	case req.IsNil() || req.JoinerSig == nil:
		return errors.New("join request is not signed")
	case !req.Carrier.IsEqual(c.self):
		return errNotJoinCarrier
	case !req.Tier.IsValid():
		return errors.New("join request names an invalid tier")
	case req.FiledAt.Time().After(c.now()):
		return errFiledLater
	}

	m, ok := c.view.Membership(c.self)
	if !ok || (m.Tier != user.TierTrusted && m.Tier != user.TierPrivilegedMember) {
		return errCarrierTier
	}
	if req.Tier != user.TierPrivilegedMember && !swarmset.MayIssue(m.Tier, req.Tier) {
		return errTierAboveIssuer
	}
	return nil
}

// carriesJoins reports whether id is a member that may carry JoinRequests and
// file ExpelRequests: Trusted or Privileged.
func carriesJoins(view membershipReader, id *astral.Identity) bool {
	m, ok := view.Membership(id)
	return ok && (m.Tier == user.TierTrusted || m.Tier == user.TierPrivilegedMember)
}

// open refuses a request that is closed: declined, which covers an expelled
// joiner, or past its Timeout. A fulfilled request stays open, so a resent
// request gets its stored membership.
func (c *swarmCarrier) open(req *user.SignedJoinRequest, reqID *astral.ObjectID) error {
	switch c.view.RequestStatus(reqID) {
	case user.RequestFulfilled:
		return nil
	case user.RequestDeclined:
		return errJoinDeclined
	}
	if req.Expired(c.now()) {
		return errJoinExpired
	}
	return nil
}

// issue returns the joiner's membership signed by its issuer alone: stored,
// the one already stored for req, or a new one this node signs, or, at tier
// privileged_member, the user key's.
func (c *swarmCarrier) issue(ctx *astral.Context, req *user.SignedJoinRequest, reqID *astral.ObjectID, stored *auth.SignedContract) (*auth.SignedContract, error) {
	if stored != nil {
		return &auth.SignedContract{Contract: stored.Contract, IssuerSig: stored.IssuerSig}, nil
	}
	if req.Tier == user.TierPrivilegedMember {
		return c.carryPrivileged(ctx, req, reqID)
	}
	return c.signMembership(ctx, req, reqID)
}

// stored returns the membership with both signatures stored for reqID that
// admits req's joiner at req's tier, or nil.
//
// why: a joiner that resends its request after the membership was stored
// gets that membership again, with no second signature of the user key and no
// second entry: a signature differs on every signing.
func (c *swarmCarrier) stored(req *user.SignedJoinRequest, reqID *astral.ObjectID) *auth.SignedContract {
	for _, sc := range c.view.ContractsOf(reqID) {
		if sc.IsNil() || sc.SubjectSig == nil || !sc.Subject.IsEqual(req.Joiner) {
			continue
		}
		if user.TierOnly(sc.Contract, req.Tier) {
			return sc
		}
	}
	return nil
}

// signMembership signs the joiner's membership at the requested tier as this
// node, naming reqID as its RequestID. It expires at the request's FiledAt
// plus the carrier's validity, or with this node's own membership, if
// earlier, so a resent request gets the same body.
func (c *swarmCarrier) signMembership(ctx *astral.Context, req *user.SignedJoinRequest, reqID *astral.ObjectID) (*auth.SignedContract, error) {
	var bound astral.Time
	if m, ok := c.view.Membership(c.self); ok {
		bound = m.ExpiresAt
	}
	expires := user.JoinMembershipExpiry(req.JoinRequest, c.validity, bound)
	contract, err := user.NewMembershipUntil(c.self, req.Joiner, req.Tier, reqID, expires)
	if err != nil {
		return nil, err
	}

	sig, err := c.signer.Sign(ctx, secp256k1.FromIdentity(c.self), contract)
	if err != nil {
		return nil, err
	}
	return &auth.SignedContract{Contract: contract, IssuerSig: sig}, nil
}

// complete stores the membership with the issuer's signature from half and
// the subject's from signed, which must carry the body of half, and returns
// it; the merge check verifies both signatures. A membership stored before
// is returned as stored.
func (c *swarmCarrier) complete(ctx *astral.Context, half, signed, stored *auth.SignedContract) (*auth.SignedContract, error) {
	if signed.IsNil() || signed.SubjectSig == nil || !sameBody(half.Contract, signed.Contract) {
		return nil, errNotSignedByJoiner
	}
	if stored != nil {
		return stored, nil
	}
	sc := &auth.SignedContract{Contract: half.Contract, IssuerSig: half.IssuerSig, SubjectSig: signed.SubjectSig}
	if err := c.state.AddContract(ctx, sc); err != nil {
		if errors.Is(err, usermod.ErrSwarmEntryDropped) {
			return nil, errNotSignedByJoiner
		}
		return nil, err
	}
	return sc, nil
}

// sameBody reports whether a and b are the same unsigned contract.
func sameBody(a, b *auth.Contract) bool {
	ida, err := astral.ResolveObjectID(a)
	if err != nil {
		return false
	}
	idb, err := astral.ResolveObjectID(b)
	return err == nil && ida.IsEqual(idb)
}

// carryPrivileged has the user key sign a Privileged-tier membership in a
// ceremony (D2). The user key signs only after a user action: a Coldcard
// press, or, with a software user key, the user's user.confirm_join, which
// this carrier waits for.
func (c *swarmCarrier) carryPrivileged(ctx *astral.Context, req *user.SignedJoinRequest, reqID *astral.ObjectID) (*auth.SignedContract, error) {
	if c.ceremony == nil {
		return nil, errUserKeyRequired
	}
	if c.ceremony.devicePress {
		return c.ceremony.Membership(ctx, req.Joiner, user.TierPrivilegedMember, req)
	}
	return c.awaitConfirmation(ctx, req, reqID)
}

// awaitConfirmation polls until the user, through user.confirm_join, has
// signed the joiner's PrivilegedMembership, and returns it. A Decline or the
// request's Timeout ends the wait first; an ended ctx pauses it.
func (c *swarmCarrier) awaitConfirmation(ctx *astral.Context, req *user.SignedJoinRequest, reqID *astral.ObjectID) (*auth.SignedContract, error) {
	ticker := time.NewTicker(c.poll)
	defer ticker.Stop()

	var head uint64
	for first := true; ; first = false {
		if sc := c.confirmed.get(reqID); sc != nil {
			return sc, nil
		}
		if h := c.state.SwarmHead(); first || h != head {
			head = h
			if c.view.RequestStatus(reqID) == user.RequestDeclined {
				return nil, errJoinDeclined
			}
		}
		if req.Expired(c.now()) {
			return nil, errJoinExpired
		}

		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-ticker.C:
		}
	}
}

// joinConfirmations holds the PrivilegedMemberships the user signed with
// user.confirm_join on this node, by the entry ID of their JoinRequest, until
// the carrier hands one to its joiner. It lives in memory: after a restart the
// user confirms again, and the body is the same.
type joinConfirmations struct {
	m sig.Map[string, *auth.SignedContract]
}

func (j *joinConfirmations) get(reqID *astral.ObjectID) *auth.SignedContract {
	if j == nil {
		return nil
	}
	sc, _ := j.m.Get(reqID.String())
	return sc
}

// keep stores sc for reqID unless one is kept already, and returns the one kept.
func (j *joinConfirmations) keep(reqID *astral.ObjectID, sc *auth.SignedContract) *auth.SignedContract {
	kept, _ := j.m.Set(reqID.String(), sc)
	return kept
}
