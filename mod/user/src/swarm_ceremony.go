package user

import (
	"errors"
	"time"

	"github.com/astralp2p/astral-go/api/auth"
	"github.com/astralp2p/astral-go/api/secp256k1"
	"github.com/astralp2p/astral-go/api/user"
	"github.com/astralp2p/astral-go/astral"
	cryptomod "github.com/astralp2p/astrald/mod/crypto"
	usermod "github.com/astralp2p/astrald/mod/user"
	"github.com/astralp2p/astrald/mod/user/src/swarmset"
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
type userKeyCeremony struct {
	user   *astral.Identity
	signer cryptomod.Module
	mod    usermod.Module
	// devicePress is set when signer asks the user for every signature (a
	// Coldcard). The zero value is the safe one: wait for user.confirm_join.
	devicePress bool
	now         func() time.Time
}

func newUserKeyCeremony(userID *astral.Identity, signer cryptomod.Module, mod usermod.Module) *userKeyCeremony {
	return &userKeyCeremony{user: userID, signer: signer, mod: mod, now: time.Now}
}

// Membership signs subject's membership at tier with the user key, as its
// issuer only: the subject co-signs it. req is the JoinRequest it answers,
// in the carrier session, and nil at bootstrap. A membership answering req
// names req's entry ID as its RequestID and expires at req's FiledAt plus the
// ceremony's validity, so a resent request gets the same body; one answering
// no request expires after the validity.
func (c *userKeyCeremony) Membership(ctx *astral.Context, subject *astral.Identity, tier user.Tier, req *user.SignedJoinRequest) (*auth.SignedContract, error) {
	// why no cap at the issuer: the user key holds no membership.
	var contract *auth.Contract
	var err error
	if req.IsNil() {
		contract, err = user.NewMembership(c.user, subject, tier, nil, usermod.DefaultContractValidity)
	} else {
		var reqID *astral.ObjectID
		if reqID, err = astral.ResolveObjectID(req); err != nil {
			return nil, err
		}
		expires := user.JoinMembershipExpiry(req.JoinRequest, usermod.DefaultContractValidity, astral.Time{})
		contract, err = user.NewMembershipUntil(c.user, subject, tier, reqID, expires)
	}
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
	if err = c.mod.AddExpulsion(ctx, signed); err != nil {
		return nil, err
	}
	return signed, nil
}

// expelRequest reads the ExpelRequest reqID and checks that the user key may
// carry it out.
func (c *userKeyCeremony) expelRequest(reqID *astral.ObjectID) (*user.SignedExpelRequest, error) {
	obj, err := c.mod.SwarmEntry(reqID)
	if err != nil {
		return nil, errNoExpelRequest
	}
	req, ok := obj.(*user.SignedExpelRequest)
	if !ok || req.IsNil() {
		return nil, errNoExpelRequest
	}

	// why the user key as filer: the root key may write every kind of entry.
	filer, ok := c.mod.Membership(req.FiledBy)
	switch {
	case !req.FiledBy.IsEqual(c.user) && !(ok && swarmset.TrustedOrAbove(filer.Tier)):
		return nil, errExpelFilerTier
	case req.Subject.IsEqual(c.user):
		return nil, errors.New("the user key is not expelled")
	case c.mod.Expelled(req.Subject):
		return nil, errAlreadyExpelled
	case c.mod.RequestStatus(reqID) == user.RequestDeclined:
		return nil, errExpelDeclined
	case req.Expired(c.now()):
		return nil, errExpelExpired
	}
	return req, nil
}
