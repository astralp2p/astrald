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
	cryptomod "github.com/astralp2p/astrald/mod/crypto"
	usermod "github.com/astralp2p/astrald/mod/user"
	"github.com/astralp2p/astrald/mod/user/src/swarmset"
)

// offerValidity is how long an offer that answers a ContractRequest holds at most: 3650 days after signing.
// Spec: .ai/system/protocols/user/README.md#app-grants
const offerValidity = 3650 * 24 * time.Hour

// writerView is the part of usermod.SwarmView the typed writes check before they sign.
type writerView interface {
	RequestStatus(req *astral.ObjectID) user.RequestStatus
	Membership(id *astral.Identity) (*user.Membership, bool)
	MayAnswer(signer *astral.Identity, req *astral.ObjectID) bool
}

// swarmWriter makes the typed writes of usermod.SwarmState: each checks whether this node may make it, signs
// what this node signs with its key, and ends in merge, the one merge of the store.
type swarmWriter struct {
	self   *astral.Identity
	signer cryptomod.Module
	view   writerView
	// entry reads a stored entry (usermod.SwarmState.SwarmEntry): the request Approve answers.
	entry func(id *astral.ObjectID) (astral.Object, error)
	// merge is the store's merge (Module.mergeSwarmEntry in the running node).
	merge func(ctx *astral.Context, obj astral.Object) error
}

// as returns w signing as id with signer: the user key's writes (user.answer_request with as=user).
func (w *swarmWriter) as(id *astral.Identity, signer cryptomod.Module) *swarmWriter {
	c := *w
	c.self, c.signer = id, signer
	return &c
}

// add merges obj. A held entry counts as written: it is signed, and a later merge stores it.
func (w *swarmWriter) add(ctx *astral.Context, obj astral.Object) error {
	if err := w.merge(ctx, obj); err != nil && !errors.Is(err, usermod.ErrSwarmEntryHeld) {
		return err
	}
	return nil
}

// sign signs body with this node's key.
func (w *swarmWriter) sign(ctx *astral.Context, body crypto.SignableTextObject) (*crypto.Signature, error) {
	return w.signer.Sign(ctx, secp256k1.FromIdentity(w.self), body)
}

// FileContractRequest signs and adds the ContractRequest that asks for permits for app, filed by this signer.
func (w *swarmWriter) FileContractRequest(ctx *astral.Context, app *astral.Identity, permits []*auth.Permit) (*user.SignedContractRequest, error) {
	if app.IsZero() || len(permits) == 0 {
		return nil, errors.New("a contract request names an app and permits")
	}
	req := user.NewContractRequest(w.self, app, permits)
	sig, err := w.sign(ctx, req)
	if err != nil {
		return nil, fmt.Errorf("sign contract request: %w", err)
	}
	signed := &user.SignedContractRequest{ContractRequest: req, FiledBySig: sig}
	return signed, w.add(ctx, signed)
}

// FileJoinRequest adds req, a JoinRequest this node carries; only a Trusted or Privileged member carries one.
func (w *swarmWriter) FileJoinRequest(ctx *astral.Context, req *user.SignedJoinRequest) (*astral.ObjectID, error) {
	m, ok := w.view.Membership(w.self)
	if req.IsNil() || !req.Carrier.IsEqual(w.self) || !ok || !swarmset.TrustedOrAbove(m.Tier) {
		return nil, usermod.ErrNotAllowed
	}
	id, err := astral.ResolveObjectID(req)
	if err != nil {
		return nil, err
	}
	return id, w.add(ctx, req)
}

// FileExpelRequest signs and adds an ExpelRequest of subject; only a Trusted or Privileged member files one.
func (w *swarmWriter) FileExpelRequest(ctx *astral.Context, subject *astral.Identity) (*user.SignedExpelRequest, error) {
	if m, ok := w.view.Membership(w.self); !ok || !swarmset.TrustedOrAbove(m.Tier) {
		return nil, fmt.Errorf("%w: %w", usermod.ErrNotAllowed, errExpelFilerTier)
	}
	req := user.NewExpelRequest(w.self, subject)
	sig, err := w.sign(ctx, req)
	if err != nil {
		return nil, err
	}
	signed := &user.SignedExpelRequest{ExpelRequest: req, FiledBySig: sig}
	return signed, w.add(ctx, signed)
}

// Approve answers the open request with entry ID request with a yes: an offer for a ContractRequest and an
// Approval for an ExpelRequest. It returns the entry it added.
func (w *swarmWriter) Approve(ctx *astral.Context, request *astral.ObjectID) (astral.Object, error) {
	if !w.view.MayAnswer(w.self, request) {
		return nil, usermod.ErrNotAllowed
	}
	obj, err := w.entry(request)
	if err != nil {
		return nil, err
	}
	// why: only an open request takes an answer; a join has no Approval, its carrier signs the membership.
	status := w.view.RequestStatus(request)
	switch req := obj.(type) {
	case *user.SignedContractRequest:
		if status == user.RequestPending || status == user.RequestOffered {
			return w.offer(ctx, request, req)
		}
	case *user.SignedExpelRequest:
		if status == user.RequestPending {
			return w.approval(ctx, request)
		}
	}
	return nil, usermod.ErrNotAllowed
}

// approval signs and adds an Approval of the ExpelRequest with entry ID request.
func (w *swarmWriter) approval(ctx *astral.Context, request *astral.ObjectID) (*user.SignedApproval, error) {
	body := user.NewApproval(w.self, request)
	sig, err := w.sign(ctx, body)
	if err != nil {
		return nil, err
	}
	signed := &user.SignedApproval{Approval: body, IssuerSig: sig}
	return signed, w.add(ctx, signed)
}

// offer signs, as issuer only, the offer that answers the ContractRequest req with entry ID reqID: a contract
// from this signer to req's filer with every requested permit at one more Delegation, naming reqID, and adds
// it. The filer completes it.
// Spec: .ai/system/protocols/user/README.md#app-grants
func (w *swarmWriter) offer(ctx *astral.Context, reqID *astral.ObjectID, req *user.SignedContractRequest) (*auth.SignedContract, error) {
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
		Issuer:    w.self,
		Subject:   req.FiledBy,
		Permits:   permits,
		ExpiresAt: astral.Time(w.offerExpiry()),
		RequestID: reqID,
	}
	sig, err := w.sign(ctx, contract)
	if err != nil {
		return nil, err
	}
	signed := &auth.SignedContract{Contract: contract, IssuerSig: sig}
	return signed, w.add(ctx, signed)
}

// offerExpiry returns when an offer signed now expires: after offerValidity, or when this signer's
// membership expires, if earlier. The user key holds no membership.
//
// why: a contract never outlives its issuer's own authority.
func (w *swarmWriter) offerExpiry() time.Time {
	expires := time.Now().Add(offerValidity)
	if m, ok := w.view.Membership(w.self); ok && m.SignedContract != nil && m.Contract != nil {
		if t := m.ExpiresAt.Time(); t.Before(expires) {
			expires = t
		}
	}
	return expires
}

// Decline signs and adds a Decline of the open request with entry ID request.
func (w *swarmWriter) Decline(ctx *astral.Context, request *astral.ObjectID) (*user.SignedDecline, error) {
	if !w.view.MayAnswer(w.self, request) {
		return nil, usermod.ErrNotAllowed
	}
	// why: nobody answers a fulfilled, expired or declined request.
	if status := w.view.RequestStatus(request); status != user.RequestPending && status != user.RequestOffered && status != user.RequestApproved {
		return nil, usermod.ErrNotAllowed
	}
	body := user.NewDecline(w.self, request)
	sig, err := w.sign(ctx, body)
	if err != nil {
		return nil, err
	}
	signed := &user.SignedDecline{Decline: body, IssuerSig: sig}
	return signed, w.add(ctx, signed)
}

// AddContract adds sc, a contract signed elsewhere, to SwarmState.
func (w *swarmWriter) AddContract(ctx *astral.Context, sc *auth.SignedContract) error {
	if sc.IsNil() {
		return errors.New("empty contract")
	}
	return w.add(ctx, sc)
}

// AddExpulsion adds e, an Expulsion signed elsewhere, to SwarmState.
func (w *swarmWriter) AddExpulsion(ctx *astral.Context, e *user.SignedExpulsion) error {
	if e.IsNil() {
		return errors.New("empty expulsion")
	}
	return w.add(ctx, e)
}

// The typed writes of usermod.SwarmState; each refuses before wireSwarm built the writer.

// FileContractRequest files a ContractRequest for app (usermod.SwarmState).
func (mod *Module) FileContractRequest(ctx *astral.Context, app *astral.Identity, permits []*auth.Permit) (*user.SignedContractRequest, error) {
	if mod.writer == nil {
		return nil, swarmset.ErrNotOpen
	}
	return mod.writer.FileContractRequest(ctx, app, permits)
}

// FileJoinRequest files req and returns its entry ID (usermod.SwarmState).
func (mod *Module) FileJoinRequest(ctx *astral.Context, req *user.SignedJoinRequest) (*astral.ObjectID, error) {
	if mod.writer == nil {
		return nil, swarmset.ErrNotOpen
	}
	return mod.writer.FileJoinRequest(ctx, req)
}

// FileExpelRequest files an ExpelRequest of subject (usermod.SwarmState).
func (mod *Module) FileExpelRequest(ctx *astral.Context, subject *astral.Identity) (*user.SignedExpelRequest, error) {
	if mod.writer == nil {
		return nil, swarmset.ErrNotOpen
	}
	return mod.writer.FileExpelRequest(ctx, subject)
}

// Approve answers a request with a yes (usermod.SwarmState).
func (mod *Module) Approve(ctx *astral.Context, request *astral.ObjectID) (astral.Object, error) {
	if mod.writer == nil {
		return nil, swarmset.ErrNotOpen
	}
	return mod.writer.Approve(ctx, request)
}

// Decline declines a request (usermod.SwarmState).
func (mod *Module) Decline(ctx *astral.Context, request *astral.ObjectID) (*user.SignedDecline, error) {
	if mod.writer == nil {
		return nil, swarmset.ErrNotOpen
	}
	return mod.writer.Decline(ctx, request)
}

// AddContract adds a signed contract (usermod.SwarmState).
func (mod *Module) AddContract(ctx *astral.Context, sc *auth.SignedContract) error {
	if mod.writer == nil {
		return swarmset.ErrNotOpen
	}
	return mod.writer.AddContract(ctx, sc)
}

// AddExpulsion adds a signed Expulsion (usermod.SwarmState).
func (mod *Module) AddExpulsion(ctx *astral.Context, e *user.SignedExpulsion) error {
	if mod.writer == nil {
		return swarmset.ErrNotOpen
	}
	return mod.writer.AddExpulsion(ctx, e)
}
