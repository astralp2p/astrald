package user

import (
	"errors"
	"time"

	"github.com/astralp2p/astral-go/api/auth"
	"github.com/astralp2p/astral-go/api/crypto"
	"github.com/astralp2p/astral-go/api/secp256k1"
	"github.com/astralp2p/astral-go/api/user"
	"github.com/astralp2p/astral-go/astral"
	secp256k1sig "github.com/astralp2p/astrald/mod/secp256k1"
	usermod "github.com/astralp2p/astrald/mod/user"
)

// The ends of an expel ceremony.
var (
	errNoExpelRequest  = errors.New("no stored expel request with that ID")
	errExpelFilerTier  = errors.New("only a trusted or privileged member files an expel request")
	errExpelDeclined   = errors.New("expel request declined")
	errExpelExpired    = errors.New("expel request expired")
	errAlreadyExpelled = errors.New("subject is already expelled")
)

// userKeyCeremony is where the user key signs: the software key held on this
// node, or a Coldcard, which signs the entry's BIP-137 text and shows its tier
// word (e.g. privileged_member). Only the user key signs a PrivilegedMembership
// (D2) and an Expulsion (D7).
//
// A PrivilegedMembership is signed only after a user action. On a Coldcard
// the press is that action, so devicePress is set and a carrier signs the
// moment a joiner asks. A software user key held on this node signs with no
// action at all, so devicePress is clear and the membership waits for the
// user to call user.confirm_join.
//
// note: nothing in the running node builds a userKeyCeremony until G7.
type userKeyCeremony struct {
	user   *astral.Identity
	signer entrySigner
	// devicePress is set when signer asks the user for every signature (a
	// Coldcard). The zero value is the safe one: wait for user.confirm_join.
	devicePress bool
	state       ceremonyState     // nil: the ceremony signs memberships only
	view        usermod.SwarmView // nil: the ceremony signs memberships only
	validity    time.Duration
	now         func() time.Time
}

// ceremonyState is the part of usermod.SwarmState an expel ceremony uses.
type ceremonyState interface {
	SwarmEntry(id *astral.ObjectID) (astral.Object, error)
	AddExpulsion(ctx *astral.Context, e *user.SignedExpulsion) error
}

func newUserKeyCeremony(userID *astral.Identity, signer entrySigner, state ceremonyState, view usermod.SwarmView) *userKeyCeremony {
	return &userKeyCeremony{
		user:     userID,
		signer:   signer,
		state:    state,
		view:     view,
		validity: usermod.DefaultContractValidity,
		now:      time.Now,
	}
}

// userKeyMembershipLink returns the ApprovalID of a membership the user key
// signs in a ceremony, for the JoinRequest joinReq (nil for the first node).
//
// why nil: provisional option (c) of the parent ticket's open question on what
// later Privileged memberships link to, taken by intern0 on 2026-11-25 under
// the operator's instruction to continue; every user-key membership carries no
// link. Another answer changes this function alone.
func userKeyMembershipLink(joinReq *astral.ObjectID) *astral.ObjectID {
	return nil
}

// Membership signs subject's membership at tier with the user key. joinReq is
// the JoinRequest it answers, nil at bootstrap.
func (c *userKeyCeremony) Membership(ctx *astral.Context, subject *astral.Identity, tier user.Tier, joinReq *astral.ObjectID) (*auth.SignedContract, error) {
	contract, err := user.NewMembership(c.user, subject, tier, userKeyMembershipLink(joinReq), c.validity)
	if err != nil {
		return nil, err
	}

	sig, err := c.signer.Sign(ctx, secp256k1.FromIdentity(c.user), contract)
	if err != nil {
		return nil, err
	}
	return &auth.SignedContract{Contract: contract, IssuerSig: sig}, nil
}

// Expel signs, as the user key, the Expulsion that carries out the stored
// ExpelRequest reqID, and adds it to SwarmState. The request must be filed by a
// Trusted or Privileged member, and be neither declined nor expired.
func (c *userKeyCeremony) Expel(ctx *astral.Context, reqID *astral.ObjectID) (*user.SignedExpulsion, error) {
	req, err := c.expelRequest(reqID)
	if err != nil {
		return nil, err
	}

	expulsion := &user.Expulsion{
		Issuer:     c.user,
		Subject:    req.Subject,
		ExpelledAt: astral.Time(c.now()),
	}
	sig, err := c.signer.Sign(ctx, secp256k1.FromIdentity(c.user), expulsion)
	if err != nil {
		return nil, err
	}

	signed := &user.SignedExpulsion{Expulsion: expulsion, IssuerSig: sig}
	if err = c.state.AddExpulsion(ctx, signed); err != nil {
		return nil, err
	}
	return signed, nil
}

// expelRequest reads the ExpelRequest reqID and checks that the user key may
// carry it out.
func (c *userKeyCeremony) expelRequest(reqID *astral.ObjectID) (*user.SignedExpelRequest, error) {
	obj, err := c.state.SwarmEntry(reqID)
	if err != nil {
		return nil, errNoExpelRequest
	}
	req, ok := obj.(*user.SignedExpelRequest)
	if !ok || req.IsNil() {
		return nil, errNoExpelRequest
	}

	// why the user key as filer: the root key may write every kind of entry.
	switch {
	case !req.FiledBy.IsEqual(c.user) && !carriesJoins(c.view, req.FiledBy):
		return nil, errExpelFilerTier
	case req.Subject.IsEqual(c.user):
		return nil, errors.New("the user key is not expelled")
	case c.view.Expelled(req.Subject):
		return nil, errAlreadyExpelled
	case c.view.RequestStatus(reqID) == user.RequestDeclined:
		return nil, errExpelDeclined
	case req.Expired(c.now()):
		return nil, errExpelExpired
	}
	return req, nil
}

// heldKeySigner signs as the one private key it holds, in memory. It is the
// software user key of a bootstrap, before the key is stored.
type heldKeySigner struct {
	key *crypto.PrivateKey
}

func (s heldKeySigner) Sign(_ *astral.Context, key *crypto.PublicKey, obj crypto.SignableTextObject) (*crypto.Signature, error) {
	if !secp256k1.Identity(key).IsEqual(secp256k1.Identity(secp256k1.PublicKey(s.key))) {
		return nil, errors.New("the held key is not the signing key")
	}
	return secp256k1sig.SignASN1(s.key, obj.SignableHash())
}
