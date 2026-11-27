package user

import (
	"errors"
	"sync"
	"time"

	"github.com/astralp2p/astral-go/api/auth"
	"github.com/astralp2p/astral-go/api/crypto"
	"github.com/astralp2p/astral-go/api/secp256k1"
	"github.com/astralp2p/astral-go/api/user"
	"github.com/astralp2p/astral-go/astral"
	usermod "github.com/astralp2p/astrald/mod/user"
)

// The ends of a join a carrier reports to its joiner.
var (
	errJoinDeclined    = errors.New("join request declined")
	errJoinExpired     = errors.New("join request expired")
	errNotJoinCarrier  = errors.New("join request names another carrier")
	errCarrierTier     = errors.New("only a trusted or privileged member carries a join request")
	errUserKeyRequired = errors.New("a privileged membership needs the user key's signature")
)

// defaultSwarmPoll is how often a carrier reads SwarmState's head while it
// waits for an answer.
// todo: switch to vfs.watch when it lands.
const defaultSwarmPoll = 2 * time.Second

// entrySigner signs a SwarmState entry as key. crypto.Module.Sign is one: it
// signs the hash with a held key, or the BIP-137 text on a Coldcard.
type entrySigner interface {
	Sign(ctx *astral.Context, key *crypto.PublicKey, obj crypto.SignableTextObject) (*crypto.Signature, error)
}

// swarmCarrier carries a joiner's JoinRequest into SwarmState and hands the
// joiner its membership. It keeps nothing between connections: a joiner that
// reconnects resends the same request and the carrier runs the same steps.
// Spec: the "Resuming a join" flow of the SwarmState working paper.
//
// note: nothing in the running node builds a swarmCarrier until G7.
type swarmCarrier struct {
	self     *astral.Identity
	state    carrierState
	view     usermod.SwarmView
	signer   entrySigner
	ceremony *userKeyCeremony // nil: this node cannot reach the user key
	validity time.Duration    // of the memberships it signs
	poll     time.Duration
	now      func() time.Time
	mu       *sync.Mutex // serializes carrying out, so one Approval yields one contract
}

// carrierState is the part of usermod.SwarmState a carrier uses.
type carrierState interface {
	SwarmHead() uint64
	FileJoinRequest(ctx *astral.Context, req *user.SignedJoinRequest) (*astral.ObjectID, error)
	AddContract(ctx *astral.Context, sc *auth.SignedContract) error
}

func newSwarmCarrier(self *astral.Identity, state carrierState, view usermod.SwarmView, signer entrySigner) *swarmCarrier {
	return &swarmCarrier{
		self:     self,
		state:    state,
		view:     view,
		signer:   signer,
		validity: usermod.DefaultContractValidity,
		poll:     defaultSwarmPoll,
		now:      time.Now,
		mu:       &sync.Mutex{},
	}
}

// withCeremony returns this carrier with ceremony as the user key it reaches,
// sharing its lock.
//
// why per carry: the user key is stored on, or removed from, this node while
// it runs, so the ceremony is read when a join arrives.
func (c *swarmCarrier) withCeremony(ceremony *userKeyCeremony) *swarmCarrier {
	cc := *c
	cc.ceremony = ceremony
	return &cc
}

// Carry writes req into SwarmState, waits until it is answered and returns the
// joiner's membership. A Decline or the request's Timeout ends the join with
// errJoinDeclined or errJoinExpired. An ended ctx (a dropped connection) only
// pauses it: the joiner resends req.
func (c *swarmCarrier) Carry(ctx *astral.Context, req *user.SignedJoinRequest) (*auth.SignedContract, error) {
	if err := c.admit(req); err != nil {
		return nil, err
	}

	reqID, err := c.write(ctx, req)
	if err != nil {
		return nil, err
	}

	if req.Tier == user.TierPrivilegedMember {
		return c.carryPrivileged(ctx, req, reqID)
	}

	switch c.awaitAnswer(ctx, req, reqID) {
	case user.RequestApproved, user.RequestFulfilled:
		return c.carryOut(ctx, req, reqID)
	case user.RequestDeclined:
		return nil, errJoinDeclined
	case user.RequestExpired:
		return nil, errJoinExpired
	}
	return nil, ctx.Err()
}

// admit refuses a request this node may not carry: one naming another
// carrier, an invalid tier, or a carrier below Trusted (D5, D10).
func (c *swarmCarrier) admit(req *user.SignedJoinRequest) error {
	switch {
	case req.IsNil() || req.JoinerSig == nil:
		return errors.New("join request is not signed")
	case !req.Carrier.IsEqual(c.self):
		return errNotJoinCarrier
	case !req.Tier.IsValid():
		return errors.New("join request names an invalid tier")
	}

	if !carriesJoins(c.view, c.self) {
		return errCarrierTier
	}
	return nil
}

// carriesJoins reports whether id is a member that may carry JoinRequests and
// file ExpelRequests: Trusted or Privileged.
func carriesJoins(view membershipReader, id *astral.Identity) bool {
	m, ok := view.Membership(id)
	return ok && (m.Tier == user.TierTrusted || m.Tier == user.TierPrivilegedMember)
}

// write files req in SwarmState and returns its entry ID. A resent request is
// already stored, which is a no-op.
//
// why a held request is no failure: it is checked again after the next merge,
// and the wait ends at the request's own Timeout if it never enters.
func (c *swarmCarrier) write(ctx *astral.Context, req *user.SignedJoinRequest) (*astral.ObjectID, error) {
	return c.state.FileJoinRequest(ctx, req)
}

// awaitAnswer polls SwarmState's head until req is no longer pending, and
// returns its status, or "" when ctx ends first.
func (c *swarmCarrier) awaitAnswer(ctx *astral.Context, req *user.SignedJoinRequest, reqID *astral.ObjectID) user.RequestStatus {
	ticker := time.NewTicker(c.poll)
	defer ticker.Stop()

	var head uint64
	status := user.RequestPending
	for first := true; ; first = false {
		// why: only new entries change an answer, so the view is read when the
		// head moves; the Timeout is read from the carrier's own copy.
		if h := c.state.SwarmHead(); first || h != head {
			head = h
			status = c.view.RequestStatus(reqID)
		}
		if status == user.RequestPending && req.Expired(c.now()) {
			status = user.RequestExpired
		}
		if status != user.RequestPending {
			return status
		}

		select {
		case <-ctx.Done():
			return ""
		case <-ticker.C:
		}
	}
}

// carryOut hands over the membership this carrier signed for req, and signs
// one when none is stored yet.
func (c *swarmCarrier) carryOut(ctx *astral.Context, req *user.SignedJoinRequest, reqID *astral.ObjectID) (*auth.SignedContract, error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	if sc := c.carriedOut(reqID, req.Joiner); sc != nil {
		return sc, nil
	}
	return c.signMembership(ctx, req, reqID)
}

// carriedOut returns the membership this carrier already signed for joiner
// answering reqID, or nil.
func (c *swarmCarrier) carriedOut(reqID *astral.ObjectID, joiner *astral.Identity) *auth.SignedContract {
	for _, sc := range c.view.ContractsOf(reqID) {
		if !sc.IsNil() && sc.Issuer.IsEqual(c.self) && sc.Subject.IsEqual(joiner) {
			return sc
		}
	}
	return nil
}

// signMembership signs the joiner's membership at the requested tier,
// answering reqID, stores it in SwarmState and returns it.
func (c *swarmCarrier) signMembership(ctx *astral.Context, req *user.SignedJoinRequest, reqID *astral.ObjectID) (*auth.SignedContract, error) {
	contract, err := user.NewMembership(c.self, req.Joiner, req.Tier, reqID, c.membershipValidity())
	if err != nil {
		return nil, err
	}

	sig, err := c.signer.Sign(ctx, secp256k1.FromIdentity(c.self), contract)
	if err != nil {
		return nil, err
	}

	sc := &auth.SignedContract{Contract: contract, IssuerSig: sig}
	if err = c.state.AddContract(ctx, sc); err != nil {
		return nil, err
	}
	return sc, nil
}

// membershipValidity is the carrier's validity, cut to the carrier's own
// membership, since a link never outlives its parent.
func (c *swarmCarrier) membershipValidity() time.Duration {
	m, ok := c.view.Membership(c.self)
	if !ok {
		return c.validity
	}
	return min(c.validity, m.ExpiresAt.Time().Sub(c.now()))
}

// carryPrivileged ends a Privileged-tier join: the user key signs the
// membership in a ceremony, and no Approval stands in for it (D2). The user
// key signs only after a user action: a Coldcard press, or, with a software
// user key, the user's user.confirm_join, which this carrier waits for.
func (c *swarmCarrier) carryPrivileged(ctx *astral.Context, req *user.SignedJoinRequest, reqID *astral.ObjectID) (*auth.SignedContract, error) {
	if c.ceremony == nil {
		return nil, errUserKeyRequired
	}
	if !c.ceremony.devicePress {
		return c.awaitConfirmation(ctx, req, reqID)
	}

	switch {
	case c.view.RequestStatus(reqID) == user.RequestDeclined:
		return nil, errJoinDeclined
	case req.Expired(c.now()):
		return nil, errJoinExpired
	}

	if sc := userKeyMembership(c.view, c.ceremony.user, req.Joiner); sc != nil {
		return sc, nil
	}

	sc, err := c.ceremony.Membership(ctx, req.Joiner, user.TierPrivilegedMember, reqID)
	if err != nil {
		return nil, err
	}
	if err = c.state.AddContract(ctx, sc); err != nil {
		return nil, err
	}
	return sc, nil
}

// awaitConfirmation polls SwarmState's head until the user, through
// user.confirm_join, has stored the joiner's PrivilegedMembership, and returns
// it. A Decline or the request's Timeout ends the wait first; an ended ctx
// pauses it, as in awaitAnswer.
func (c *swarmCarrier) awaitConfirmation(ctx *astral.Context, req *user.SignedJoinRequest, reqID *astral.ObjectID) (*auth.SignedContract, error) {
	ticker := time.NewTicker(c.poll)
	defer ticker.Stop()

	var head uint64
	for first := true; ; first = false {
		if h := c.state.SwarmHead(); first || h != head {
			head = h
			if c.view.RequestStatus(reqID) == user.RequestDeclined {
				return nil, errJoinDeclined
			}
			if sc := userKeyMembership(c.view, c.ceremony.user, req.Joiner); sc != nil {
				return sc, nil
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

// userKeyMembership returns subject's PrivilegedMembership signed by the user
// key userID, or nil.
func userKeyMembership(view membershipReader, userID, subject *astral.Identity) *auth.SignedContract {
	if m, ok := view.Membership(subject); ok && usermod.PrivilegedBy(m, userID) {
		return m.SignedContract
	}
	return nil
}
