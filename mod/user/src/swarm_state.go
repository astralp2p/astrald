package user

import (
	"errors"
	"fmt"
	"math"
	"time"

	"github.com/astralp2p/astral-go/api/auth"
	"github.com/astralp2p/astral-go/api/crypto"
	"github.com/astralp2p/astral-go/api/secp256k1"
	"github.com/astralp2p/astral-go/api/user"
	"github.com/astralp2p/astral-go/astral"
	usermod "github.com/astralp2p/astrald/mod/user"
	"github.com/astralp2p/astrald/mod/user/src/swarmset"
)

// offerValidity is how long an offer that answers a ContractRequest holds at most: 3650 days after signing.
// Spec: .ai/system/protocols/user/README.md#app-grants
const offerValidity = 3650 * 24 * time.Hour

// addSwarmEntry merges obj. A held entry counts as written: it is signed, and a later merge stores it.
func (mod *Module) addSwarmEntry(ctx *astral.Context, obj astral.Object) error {
	if err := mod.mergeSwarmEntry(ctx, obj); err != nil && !errors.Is(err, usermod.ErrSwarmEntryHeld) {
		return err
	}
	return nil
}

// signAsNode signs body with this node's key.
func (mod *Module) signAsNode(ctx *astral.Context, body crypto.SignableTextObject) (*crypto.Signature, error) {
	return mod.Crypto.Sign(ctx, secp256k1.FromIdentity(mod.node.Identity()), body)
}

// FileContractRequest signs and adds the ContractRequest that asks for permits for app, filed by this signer.
func (mod *Module) FileContractRequest(ctx *astral.Context, app *astral.Identity, permits []*auth.Permit) (*user.SignedContractRequest, error) {
	if app.IsZero() || len(permits) == 0 {
		return nil, errors.New("a contract request names an app and permits")
	}
	req := user.NewContractRequest(mod.node.Identity(), app, permits)
	sig, err := mod.signAsNode(ctx, req)
	if err != nil {
		return nil, fmt.Errorf("sign contract request: %w", err)
	}
	signed := &user.SignedContractRequest{ContractRequest: req, FiledBySig: sig}
	return signed, mod.addSwarmEntry(ctx, signed)
}

// FileJoinRequest adds req, a JoinRequest this node carries; only a Trusted or Privileged member carries one.
func (mod *Module) FileJoinRequest(ctx *astral.Context, req *user.SignedJoinRequest) (*astral.ObjectID, error) {
	m, ok := mod.Membership(mod.node.Identity())
	if req.IsNil() || !req.Carrier.IsEqual(mod.node.Identity()) || !ok || !swarmset.TrustedOrAbove(m.Tier) {
		return nil, usermod.ErrNotAllowed
	}
	id, err := astral.ResolveObjectID(req)
	if err != nil {
		return nil, err
	}
	return id, mod.addSwarmEntry(ctx, req)
}

// FileExpelRequest signs and adds an ExpelRequest of subject; only a Trusted or Privileged member files one.
func (mod *Module) FileExpelRequest(ctx *astral.Context, subject *astral.Identity) (*user.SignedExpelRequest, error) {
	if m, ok := mod.Membership(mod.node.Identity()); !ok || !swarmset.TrustedOrAbove(m.Tier) {
		return nil, fmt.Errorf("%w: %w", usermod.ErrNotAllowed, errExpelFilerTier)
	}
	req := user.NewExpelRequest(mod.node.Identity(), subject)
	sig, err := mod.signAsNode(ctx, req)
	if err != nil {
		return nil, err
	}
	signed := &user.SignedExpelRequest{ExpelRequest: req, FiledBySig: sig}
	return signed, mod.addSwarmEntry(ctx, signed)
}

// Approve answers the open request with entry ID request with a yes: an offer for a ContractRequest and an
// Approval for an ExpelRequest. It returns the entry it added.
func (mod *Module) Approve(ctx *astral.Context, request *astral.ObjectID) (astral.Object, error) {
	if !mod.MayAnswer(mod.node.Identity(), request) {
		return nil, usermod.ErrNotAllowed
	}
	obj, err := mod.SwarmEntry(request)
	if err != nil {
		return nil, err
	}
	// why: only an open request takes an answer; a join has no Approval, its carrier signs the membership.
	status := mod.RequestStatus(request)
	switch req := obj.(type) {
	case *user.SignedContractRequest:
		if status == user.RequestPending || status == user.RequestOffered {
			return mod.offer(ctx, request, req)
		}
	case *user.SignedExpelRequest:
		if status == user.RequestPending {
			return mod.approval(ctx, request)
		}
	}
	return nil, usermod.ErrNotAllowed
}

// approval signs and adds an Approval of the ExpelRequest with entry ID request.
func (mod *Module) approval(ctx *astral.Context, request *astral.ObjectID) (*user.SignedApproval, error) {
	body := user.NewApproval(mod.node.Identity(), request)
	sig, err := mod.signAsNode(ctx, body)
	if err != nil {
		return nil, err
	}
	signed := &user.SignedApproval{Approval: body, IssuerSig: sig}
	return signed, mod.addSwarmEntry(ctx, signed)
}

// offer signs, as issuer only, the offer that answers the ContractRequest req with entry ID reqID: a contract
// from this signer to req's filer with every requested permit at one more Delegation, naming reqID, and adds
// it. The filer completes it.
// Spec: .ai/system/protocols/user/README.md#app-grants
func (mod *Module) offer(ctx *astral.Context, reqID *astral.ObjectID, req *user.SignedContractRequest) (*auth.SignedContract, error) {
	permits := make([]*auth.Permit, 0, len(req.Permits))
	for _, p := range req.Permits {
		if p == nil || p.Delegation == math.MaxUint8 {
			return nil, fmt.Errorf("approve %v: a requested permit cannot be delegated further", reqID)
		}
		deeper := *p
		deeper.Delegation++
		permits = append(permits, &deeper)
	}
	contract := &auth.Contract{
		Issuer:    mod.node.Identity(),
		Subject:   req.FiledBy,
		Permits:   permits,
		ExpiresAt: astral.Time(mod.offerExpiry()),
		RequestID: reqID,
	}
	sig, err := mod.signAsNode(ctx, contract)
	if err != nil {
		return nil, err
	}
	signed := &auth.SignedContract{Contract: contract, IssuerSig: sig}
	return signed, mod.addSwarmEntry(ctx, signed)
}

// offerExpiry returns when an offer signed now expires: after offerValidity, or when this signer's
// membership expires, if earlier. The user key holds no membership.
//
// why: a contract never outlives its issuer's own authority.
func (mod *Module) offerExpiry() time.Time {
	expires := mod.now().Add(offerValidity)
	if m, ok := mod.Membership(mod.node.Identity()); ok && m.SignedContract != nil && m.Contract != nil {
		if t := m.ExpiresAt.Time(); t.Before(expires) {
			expires = t
		}
	}
	return expires
}

// Decline signs and adds a Decline of the open request with entry ID request.
func (mod *Module) Decline(ctx *astral.Context, request *astral.ObjectID) (*user.SignedDecline, error) {
	if !mod.MayAnswer(mod.node.Identity(), request) {
		return nil, usermod.ErrNotAllowed
	}
	// why: nobody answers a fulfilled, expired or declined request.
	if status := mod.RequestStatus(request); status != user.RequestPending && status != user.RequestOffered && status != user.RequestApproved {
		return nil, usermod.ErrNotAllowed
	}
	body := user.NewDecline(mod.node.Identity(), request)
	sig, err := mod.signAsNode(ctx, body)
	if err != nil {
		return nil, err
	}
	signed := &user.SignedDecline{Decline: body, IssuerSig: sig}
	return signed, mod.addSwarmEntry(ctx, signed)
}

// AddContract adds sc, a contract signed elsewhere, to SwarmState.
func (mod *Module) AddContract(ctx *astral.Context, sc *auth.SignedContract) error {
	if sc.IsNil() {
		return errors.New("empty contract")
	}
	return mod.addSwarmEntry(ctx, sc)
}

// AddExpulsion adds e, an Expulsion signed elsewhere, to SwarmState.
func (mod *Module) AddExpulsion(ctx *astral.Context, e *user.SignedExpulsion) error {
	if e.IsNil() {
		return errors.New("empty expulsion")
	}
	return mod.addSwarmEntry(ctx, e)
}
