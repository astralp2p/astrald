package user

import (
	"errors"
	"sync"
	"time"

	"github.com/astralp2p/astral-go/api/auth"
	"github.com/astralp2p/astral-go/api/secp256k1"
	"github.com/astralp2p/astral-go/api/user"
	"github.com/astralp2p/astral-go/astral"
	"github.com/astralp2p/astral-go/sig"
	cryptomod "github.com/astralp2p/astrald/mod/crypto"
	usermod "github.com/astralp2p/astrald/mod/user"
	"github.com/astralp2p/astrald/mod/user/src/swarmset"
)

// joinEnd is an end of a join a carrier reports to its joiner. Its text is the
// wire text, part of the protocol (astral-docs protocols/user/ops/user.carry_join.md),
// so the joiner reads a received message back as the end of the same text
// (joinEndOf).
type joinEnd string

func (e joinEnd) Error() string { return string(e) }

const (
	errJoinDeclined      joinEnd = "join request declined"
	errJoinExpired       joinEnd = "join request expired"
	errNotJoinCarrier    joinEnd = "join request names another carrier"
	errCarrierTier       joinEnd = "only a trusted or privileged member carries a join request"
	errTierAboveIssuer   joinEnd = "tier above what this node may issue"
	errUserKeyRequired   joinEnd = "a privileged membership needs the user key's signature"
	errNotSignedByJoiner joinEnd = "membership is not signed by the joiner"
	errFiledLater        joinEnd = "join request is filed later than this node's time"
)

// The faults of a join request this node refuses to carry.
var (
	errRequestUnsigned = errors.New("join request is not signed")
	errRequestTier     = errors.New("join request names an invalid tier")
)

// closesRequest reports whether the joiner drops its request on e: the join is
// over for good. Any other end, or a dropped connection, keeps the request for
// the next join.
func (e joinEnd) closesRequest() bool {
	switch e {
	case errJoinDeclined, errJoinExpired, errNotJoinCarrier, errCarrierTier, errTierAboveIssuer:
		return true
	}
	return false
}

// confirmPoll is how often a carrier reads SwarmState's head while it waits
// for the user's confirmation.
// note: the Feed owns the switch to a watch; see swarm_feed.go.
const confirmPoll = 2 * time.Second

// joinCosign is the joiner's side of a carrier session: it takes the
// membership its issuer signed and returns it signed by the joiner as
// subject.
type joinCosign func(ctx *astral.Context, half *auth.SignedContract) (*auth.SignedContract, error)

// swarmCarrier carries a joiner's JoinRequest into SwarmState and signs the
// joiner's membership with the joiner in the carrier session: the issuer
// signs, the joiner co-signs, and the carrier stores the contract with both
// signatures. It keeps nothing between connections: a joiner that reconnects
// resends the same request and the carrier answers a membership with the same
// body. Of the sessions that carry one JoinRequest, only the first to store a
// membership stores one; every other session hands over that one.
// Spec: .ai/system/protocols/user/README.md#joins
type swarmCarrier struct {
	self      *astral.Identity
	mod       usermod.Module
	signer    cryptomod.Module
	confirmed *joinConfirmations // what user.confirm_join signed on this node
	now       func() time.Time
	// mu makes the check for a stored membership and its store one step.
	mu sync.Mutex
}

// newSwarmCarrier returns the carrier of self: it issues memberships signed by signer and reads and writes
// SwarmState through mod.
func newSwarmCarrier(self *astral.Identity, mod usermod.Module, signer cryptomod.Module) *swarmCarrier {
	return &swarmCarrier{
		self:      self,
		mod:       mod,
		signer:    signer,
		confirmed: &joinConfirmations{},
		now:       time.Now,
	}
}

// Carry writes req into SwarmState and runs the carrier session: it has the
// membership signed by its issuer, has the joiner co-sign it through cosign,
// stores it with both signatures and returns it. ceremony is where the user
// key signs on this node, for a privileged_member membership; nil: this node
// cannot reach the user key (errUserKeyRequired). A Decline, the request's
// Timeout or an expelled joiner ends the join with errJoinDeclined or
// errJoinExpired, and no membership is signed for it. An ended ctx (a dropped
// connection) only pauses it: the joiner resends req. The membership cosign
// returns must carry the body this node had signed, with the joiner's
// signature (errNotSignedByJoiner).
func (c *swarmCarrier) Carry(ctx *astral.Context, req *user.SignedJoinRequest, ceremony *userKeyCeremony, cosign joinCosign) (*auth.SignedContract, error) {
	if err := c.admit(req); err != nil {
		return nil, err
	}

	reqID, err := c.mod.FileJoinRequest(ctx, req)
	if err != nil {
		return nil, err
	}
	if err = c.open(req, reqID); err != nil {
		return nil, err
	}

	half, err := c.issue(ctx, req, reqID, ceremony)
	if err != nil {
		return nil, err
	}
	signed, err := cosign(ctx, half)
	if err != nil {
		return nil, err
	}
	if signed.IsNil() || signed.SubjectSig == nil || !sameBody(half.Contract, signed.Contract) {
		return nil, errNotSignedByJoiner
	}
	return c.store(ctx, req, reqID, &auth.SignedContract{Contract: half.Contract, IssuerSig: half.IssuerSig, SubjectSig: signed.SubjectSig})
}

// admit refuses a request this node may not carry: one not signed, naming
// another carrier or an invalid tier, filed later than this node's time, a
// carrier below Trusted (D5, D10), or a tier this node's tier may not issue
// (swarmset.MayIssue); the user key issues a privileged_member membership.
func (c *swarmCarrier) admit(req *user.SignedJoinRequest) error {
	switch {
	case req.IsNil() || req.JoinerSig == nil:
		return errRequestUnsigned
	case !req.Carrier.IsEqual(c.self):
		return errNotJoinCarrier
	case !req.Tier.IsValid():
		return errRequestTier
	case req.FiledAt.Time().After(c.now()):
		return errFiledLater
	}

	m, ok := c.mod.Membership(c.self)
	if !ok || !swarmset.TrustedOrAbove(m.Tier) {
		return errCarrierTier
	}
	if req.Tier != user.TierPrivilegedMember && !swarmset.MayIssue(m.Tier, req.Tier) {
		return errTierAboveIssuer
	}
	return nil
}

// open refuses a request that is closed: its joiner expelled, declined, or past
// its Timeout. A fulfilled request stays open to its joiner while that joiner
// is not expelled, so a resent request gets its stored membership.
//
// why: the joiner is checked first, because a fulfilled request is never declined
// and an expelled joiner must not activate a membership that grants nothing.
func (c *swarmCarrier) open(req *user.SignedJoinRequest, reqID *astral.ObjectID) error {
	if c.mod.Expelled(req.Joiner) {
		return errJoinDeclined
	}
	switch c.mod.RequestStatus(reqID) {
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

// issue returns the joiner's membership signed by its issuer alone: the one
// already stored for req, or a new one this node signs. At tier
// privileged_member the user key issues it, in a ceremony (D2): it signs only
// after a user action, a Coldcard press, or, with a software user key, the
// user's user.confirm_join, which this carrier waits for.
func (c *swarmCarrier) issue(ctx *astral.Context, req *user.SignedJoinRequest, reqID *astral.ObjectID, ceremony *userKeyCeremony) (*auth.SignedContract, error) {
	if stored := c.stored(req, reqID); stored != nil {
		return &auth.SignedContract{Contract: stored.Contract, IssuerSig: stored.IssuerSig}, nil
	}
	if req.Tier != user.TierPrivilegedMember {
		return c.signMembership(ctx, req, reqID)
	}
	if ceremony == nil {
		return nil, errUserKeyRequired
	}
	if ceremony.devicePress {
		return ceremony.Membership(ctx, req.Joiner, user.TierPrivilegedMember, req)
	}
	return c.awaitConfirmation(ctx, req, reqID)
}

// stored returns the membership with both signatures stored for reqID that
// admits req's joiner at req's tier, or nil.
//
// why: a joiner that resends its request after the membership was stored
// gets that membership again, with no second signature of the user key and no
// second entry: a signature differs on every signing.
func (c *swarmCarrier) stored(req *user.SignedJoinRequest, reqID *astral.ObjectID) *auth.SignedContract {
	for _, sc := range c.mod.ContractsOf(reqID) {
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
// plus the default contract validity, or with this node's own membership, if
// earlier, so a resent request gets the same body.
func (c *swarmCarrier) signMembership(ctx *astral.Context, req *user.SignedJoinRequest, reqID *astral.ObjectID) (*auth.SignedContract, error) {
	var bound astral.Time
	if m, ok := c.mod.Membership(c.self); ok {
		bound = m.ExpiresAt
	}
	expires := user.JoinMembershipExpiry(req.JoinRequest, usermod.DefaultContractValidity, bound)
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

// store adds sc, the joiner's membership with both signatures, to SwarmState
// and returns it; the merge check verifies both signatures. When a membership
// for req is stored already, by an earlier session or one running alongside,
// it returns that one and adds nothing.
//
// why: SwarmState keys an entry by its signatures, and a signature differs on
// every signing, so two sessions of one JoinRequest would store two entries.
func (c *swarmCarrier) store(ctx *astral.Context, req *user.SignedJoinRequest, reqID *astral.ObjectID, sc *auth.SignedContract) (*auth.SignedContract, error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	if stored := c.stored(req, reqID); stored != nil {
		return stored, nil
	}
	if err := c.mod.AddContract(ctx, sc); err != nil {
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

// awaitConfirmation polls until the user, through user.confirm_join, has
// signed the joiner's PrivilegedMembership, and returns it. A Decline or the
// request's Timeout ends the wait first; an ended ctx pauses it.
func (c *swarmCarrier) awaitConfirmation(ctx *astral.Context, req *user.SignedJoinRequest, reqID *astral.ObjectID) (*auth.SignedContract, error) {
	ticker := time.NewTicker(confirmPoll)
	defer ticker.Stop()

	var head uint64
	for first := true; ; first = false {
		if sc := c.confirmed.get(reqID); sc != nil {
			return sc, nil
		}
		if h := c.mod.SwarmHead(); first || h != head {
			head = h
			if c.mod.RequestStatus(reqID) == user.RequestDeclined {
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
