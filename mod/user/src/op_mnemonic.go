package user

import (
	"errors"
	"time"

	"github.com/astralp2p/astral-go/api/auth"
	"github.com/astralp2p/astral-go/api/user"
	"github.com/astralp2p/astral-go/astral"
	"github.com/astralp2p/astral-go/astral/channel"
	"github.com/astralp2p/astral-go/lib/routing"
	objectsmod "github.com/astralp2p/astrald/mod/objects"
)

type opMnemonicArgs struct {
	In  string
	Out string
}

// OpMnemonic returns the BIP-39 words user.bootstrap kept for the node's user,
// so the user can back them up later.
// Spec: .ai/system/protocols/user/ops/user.mnemonic.md
//
// why local origins only: the words derive the user's key, so they never leave
// the node over the network.
// why the user or the manager: the words belong to the user, and the manager
// claimed the node for the user.
// why: no named action guards this op; only the node's user or the manager
// may read the words (mnemonicGate).
func (mod *Module) OpMnemonic(ctx *astral.Context, q *routing.IncomingQuery, args opMnemonicArgs) error {
	ch := q.Accept(channel.WithFormats(args.In, args.Out))
	defer ch.Close()

	userID, err := mod.mnemonicGate(q)
	if err != nil {
		return ch.Send(astral.Err(err))
	}

	objectID, err := mod.db.MnemonicID(userID)
	if err != nil {
		return ch.Send(astral.Err(err))
	}
	if objectID == nil {
		return ch.Send(user.ErrNoMnemonic)
	}

	obj, err := mod.Objects.Load(ctx, mod.Objects.ReadDefault(), objectID)
	if err != nil {
		return ch.Send(astral.Err(err))
	}

	mnemonic, ok := obj.(*user.Mnemonic)
	if !ok {
		return ch.Send(user.ErrNoMnemonic)
	}

	return ch.Send(mnemonic)
}

// OpForgetMnemonic deletes the BIP-39 words user.bootstrap kept for the node's
// user; user.mnemonic answers ErrNoMnemonic afterwards.
// Spec: .ai/system/protocols/user/ops/user.forget_mnemonic.md
//
// why: no named action guards this op; the gate of user.mnemonic applies,
// since who may read the words may drop them (mnemonicGate).
func (mod *Module) OpForgetMnemonic(ctx *astral.Context, q *routing.IncomingQuery, args opMnemonicArgs) error {
	ch := q.Accept(channel.WithFormats(args.In, args.Out))
	defer ch.Close()

	userID, err := mod.mnemonicGate(q)
	if err != nil {
		return ch.Send(astral.Err(err))
	}

	objectID, err := mod.db.MnemonicID(userID)
	if err != nil {
		return ch.Send(astral.Err(err))
	}
	if objectID == nil {
		return ch.Send(user.ErrNoMnemonic)
	}

	err = mod.Objects.DeleteUnheld(ctx, mod.Objects.WriteDefault(), objectID, time.Now())
	if err != nil && !errors.Is(err, objectsmod.ErrNotFound) {
		return ch.Send(astral.Err(err))
	}

	err = mod.db.DeleteMnemonic(userID)
	if err != nil {
		return ch.Send(astral.Err(err))
	}

	return ch.Send(&astral.Ack{})
}

// mnemonicGate admits a local query from the node's user or the manager, and
// returns the node's user.
func (mod *Module) mnemonicGate(q *routing.IncomingQuery) (*astral.Identity, error) {
	switch q.Origin() {
	case "", astral.OriginLocal:
	default:
		return nil, astral.NewErrOriginNotAllowed(q.Origin())
	}

	userID := mod.Identity()
	if userID == nil {
		return nil, user.ErrNoMnemonic
	}

	caller := q.Caller()
	if caller.IsEqual(userID) {
		return userID, nil
	}
	manager := mod.Sys.Manager()
	if !manager.IsZero() && caller.IsEqual(manager) {
		return userID, nil
	}
	return nil, auth.ErrNotPermitted
}
