package user

import (
	"errors"
	"fmt"

	"github.com/astralp2p/astral-go/api/auth"
	"github.com/astralp2p/astral-go/api/crypto"
	"github.com/astralp2p/astral-go/api/secp256k1"
	"github.com/astralp2p/astral-go/api/user"
	"github.com/astralp2p/astral-go/astral"
	usermod "github.com/astralp2p/astrald/mod/user"
)

// writerView is the part of usermod.SwarmView the typed writes check before they sign.
type writerView interface {
	Membership(id *astral.Identity) (*user.Membership, bool)
	MayAnswer(signer *astral.Identity, req *astral.ObjectID) bool
}

// swarmWriter makes the typed writes of usermod.SwarmState: each checks whether this node may make it, signs
// what this node signs with its key, and ends in merge, the one merge of the store.
type swarmWriter struct {
	self   *astral.Identity
	signer entrySigner
	view   writerView
	// merge is the store's merge (Module.mergeSwarmEntry in the running node).
	merge func(ctx *astral.Context, obj astral.Object) error
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

func (w *swarmWriter) FileJoinRequest(ctx *astral.Context, req *user.SignedJoinRequest) (*astral.ObjectID, error) {
	if req.IsNil() || !req.Carrier.IsEqual(w.self) || !carriesJoins(w.view, w.self) {
		return nil, usermod.ErrNotAllowed
	}
	id, err := astral.ResolveObjectID(req)
	if err != nil {
		return nil, err
	}
	return id, w.add(ctx, req)
}

func (w *swarmWriter) FileExpelRequest(ctx *astral.Context, subject *astral.Identity) (*user.SignedExpelRequest, error) {
	if !carriesJoins(w.view, w.self) {
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

func (w *swarmWriter) Approve(ctx *astral.Context, request *astral.ObjectID) (*user.SignedApproval, error) {
	if !w.view.MayAnswer(w.self, request) {
		return nil, usermod.ErrNotAllowed
	}
	body := user.NewApproval(w.self, request)
	sig, err := w.sign(ctx, body)
	if err != nil {
		return nil, err
	}
	signed := &user.SignedApproval{Approval: body, IssuerSig: sig}
	return signed, w.add(ctx, signed)
}

func (w *swarmWriter) Decline(ctx *astral.Context, request *astral.ObjectID) (*user.SignedDecline, error) {
	if !w.view.MayAnswer(w.self, request) {
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

func (w *swarmWriter) AddContract(ctx *astral.Context, sc *auth.SignedContract) error {
	if sc.IsNil() {
		return errors.New("empty contract")
	}
	return w.add(ctx, sc)
}

func (w *swarmWriter) AddExpulsion(ctx *astral.Context, e *user.SignedExpulsion) error {
	if e.IsNil() {
		return errors.New("empty expulsion")
	}
	return w.add(ctx, e)
}

// The typed writes of usermod.SwarmState; each refuses before wireSwarm built the writer.

func (mod *Module) FileContractRequest(ctx *astral.Context, app *astral.Identity, permits []*auth.Permit) (*user.SignedContractRequest, error) {
	if mod.writer == nil {
		return nil, errSwarmStateNotOpen
	}
	return mod.writer.FileContractRequest(ctx, app, permits)
}

func (mod *Module) FileJoinRequest(ctx *astral.Context, req *user.SignedJoinRequest) (*astral.ObjectID, error) {
	if mod.writer == nil {
		return nil, errSwarmStateNotOpen
	}
	return mod.writer.FileJoinRequest(ctx, req)
}

func (mod *Module) FileExpelRequest(ctx *astral.Context, subject *astral.Identity) (*user.SignedExpelRequest, error) {
	if mod.writer == nil {
		return nil, errSwarmStateNotOpen
	}
	return mod.writer.FileExpelRequest(ctx, subject)
}

func (mod *Module) Approve(ctx *astral.Context, request *astral.ObjectID) (*user.SignedApproval, error) {
	if mod.writer == nil {
		return nil, errSwarmStateNotOpen
	}
	return mod.writer.Approve(ctx, request)
}

func (mod *Module) Decline(ctx *astral.Context, request *astral.ObjectID) (*user.SignedDecline, error) {
	if mod.writer == nil {
		return nil, errSwarmStateNotOpen
	}
	return mod.writer.Decline(ctx, request)
}

func (mod *Module) AddContract(ctx *astral.Context, sc *auth.SignedContract) error {
	if mod.writer == nil {
		return errSwarmStateNotOpen
	}
	return mod.writer.AddContract(ctx, sc)
}

func (mod *Module) AddExpulsion(ctx *astral.Context, e *user.SignedExpulsion) error {
	if mod.writer == nil {
		return errSwarmStateNotOpen
	}
	return mod.writer.AddExpulsion(ctx, e)
}
