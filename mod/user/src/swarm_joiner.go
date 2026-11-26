package user

import (
	"errors"
	"sync"
	"time"

	"github.com/astralp2p/astral-go/api/auth"
	"github.com/astralp2p/astral-go/api/secp256k1"
	"github.com/astralp2p/astral-go/api/user"
	"github.com/astralp2p/astral-go/astral"
)

// joinKeeper keeps the joiner's signed JoinRequest across restarts. nil
// clears it.
type joinKeeper interface {
	keepJoin(req *user.SignedJoinRequest) error
}

// joinCarry is the connection to the carrier a JoinRequest names: it sends the
// request and returns the membership the carrier hands over, or the end of
// the join.
type joinCarry func(ctx *astral.Context, req *user.SignedJoinRequest) (*auth.SignedContract, error)

// swarmJoiner is the node that asks to join. It keeps one signed JoinRequest
// until it has joined, been declined, or seen it expire, and resends that same
// object, with the same entry ID, on every reconnect.
type swarmJoiner struct {
	self   *astral.Identity
	signer entrySigner
	keeper joinKeeper
	now    func() time.Time
	mu     sync.Mutex
	kept   *user.SignedJoinRequest
}

// newSwarmJoiner returns a joiner that resumes kept, the request its keeper
// restored, or nil.
func newSwarmJoiner(self *astral.Identity, signer entrySigner, keeper joinKeeper, kept *user.SignedJoinRequest) *swarmJoiner {
	return &swarmJoiner{self: self, signer: signer, keeper: keeper, now: time.Now, kept: kept}
}

// Request returns the kept request when it names carrier and tier and has not
// expired, and otherwise signs and keeps a new one.
func (j *swarmJoiner) Request(ctx *astral.Context, carrier *astral.Identity, tier user.Tier) (*user.SignedJoinRequest, error) {
	j.mu.Lock()
	kept := j.kept
	j.mu.Unlock()

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

// sign signs req as this node and keeps it.
func (j *swarmJoiner) sign(ctx *astral.Context, req *user.JoinRequest) (*user.SignedJoinRequest, error) {
	sig, err := j.signer.Sign(ctx, secp256k1.FromIdentity(j.self), req)
	if err != nil {
		return nil, err
	}

	signed := &user.SignedJoinRequest{JoinRequest: req, JoinerSig: sig}
	return signed, j.keep(signed)
}

func (j *swarmJoiner) keep(req *user.SignedJoinRequest) error {
	j.mu.Lock()
	defer j.mu.Unlock()

	if err := j.keeper.keepJoin(req); err != nil {
		return err
	}
	j.kept = req
	return nil
}

// Join sends req to its carrier through carry and returns the membership the
// carrier hands over. A membership, a Decline, an expiry or a carrier that
// refuses the request ends the join and drops req; any other error keeps req
// for the next Join.
func (j *swarmJoiner) Join(ctx *astral.Context, req *user.SignedJoinRequest, carry joinCarry) (*auth.SignedContract, error) {
	if req.Expired(j.now()) {
		return nil, errors.Join(errJoinExpired, j.keep(nil))
	}

	sc, err := carry(ctx, req)
	switch {
	case err == nil:
		if err = j.checkMembership(req, sc); err != nil {
			return nil, err
		}
		return sc, j.keep(nil)
	case errors.Is(err, errJoinDeclined), errors.Is(err, errJoinExpired),
		errors.Is(err, errNotJoinCarrier), errors.Is(err, errCarrierTier):
		return nil, errors.Join(err, j.keep(nil))
	}
	return nil, err
}

// checkMembership checks that sc admits this node at the requested tier.
//
// note: signatures and the approval link are checked when sc enters
// SwarmState (CheckSwarmEntry) and when it is read (SwarmView.Membership).
func (j *swarmJoiner) checkMembership(req *user.SignedJoinRequest, sc *auth.SignedContract) error {
	m, ok := user.MembershipOf(sc)
	switch {
	case !ok:
		return errors.New("carrier handed over no membership")
	case !m.Subject.IsEqual(j.self):
		return errors.New("carrier handed over another node's membership")
	case m.Tier != req.Tier:
		return errors.New("carrier handed over a membership at another tier")
	}
	return nil
}

// memJoinKeeper keeps the request in memory only, for a joiner that does not
// survive a restart.
type memJoinKeeper struct{}

func (memJoinKeeper) keepJoin(*user.SignedJoinRequest) error { return nil }
