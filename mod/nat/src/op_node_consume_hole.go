package nat

import (
	natmod "github.com/astralp2p/astrald/mod/nat"
	"time"

	"github.com/astralp2p/astral-go/api/auth"
	"github.com/astralp2p/astral-go/api/nat"
	natclient "github.com/astralp2p/astral-go/api/nat/client"
	"github.com/astralp2p/astral-go/astral"
	"github.com/astralp2p/astral-go/astral/channel"
	"github.com/astralp2p/astral-go/lib/astrald"
	"github.com/astralp2p/astral-go/lib/routing"
)

const takeExchangeTimeout = 5 * time.Second

type opNodeConsumeHoleArgs struct {
	Pair     astral.Nonce `query:"required"`
	Identity string

	In  string
	Out string
}

// OpNodeConsumeHole coordinates a two-phase lock-then-take exchange to hand a hole out of the pool.
// When Identity is set it acts as the initiator; otherwise it is the responder waiting for the lock signal.
func (mod *Module) OpNodeConsumeHole(ctx *astral.Context, q *routing.IncomingQuery, args opNodeConsumeHoleArgs) (err error) {
	if !mod.Auth.Authorize(ctx, &auth.AdminNetworkAction{Action: auth.NewAction(q.Caller())}) {
		return q.Reject()
	}

	ch := channel.New(q.AcceptRaw(), channel.WithFormats(args.In, args.Out))
	defer ch.Close()

	// why: the target resolves before the hole leaves the pool, so a name naming
	// no node costs no hole. Nothing puts a taken hole back once BeginLock
	// succeeds: finalizeLock closes the socket and frees the punched mapping.
	var target *astral.Identity
	if args.Identity != "" {
		target, err = mod.Dir.ResolveIdentity(args.Identity)
		if err != nil {
			return ch.Send(astral.Err(err))
		}
		if target.IsZero() {
			return ch.Send(astral.Err(natmod.ErrUnknownIdentity))
		}
	}

	hole, err := mod.pool.Take(args.Pair)
	if err != nil {
		return ch.Send(astral.Err(err))
	}

	// why: every return below leaves the hole out of the pool. A hole still in
	// StateIdle never began locking, so the pool takes it back. Past BeginLock
	// there is nothing to give back: finalizeLock frees the punched mapping, and
	// no state returns to StateIdle.
	defer func() {
		if !hole.IsIdle() {
			return
		}
		if err := mod.pool.Add(hole); err != nil {
			mod.log.Errorv(1, "return hole %v to pool: %v", hole.Nonce, err)
		}
	}()

	holeNonce := hole.Nonce

	if target != nil {
		opCtx, cancel := ctx.WithCancel()
		defer cancel()

		if !hole.BeginLock() {
			return ch.Send(astral.Err(natmod.ErrHoleBusy))
		}

		natClient := natclient.New(target, astrald.Default())
		err = natClient.NodeConsumeHole(opCtx, holeNonce, nil)
		if err != nil {
			return ch.Send(astral.Err(err))
		}

		if err := hole.WaitLocked(opCtx); err != nil {
			return ch.Send(astral.Err(err))
		}

		return ch.Send(&astral.Ack{})
	}

	// Responder flow
	opCtx, cancel := ctx.WithTimeout(hole.LockTimeout() + takeExchangeTimeout)
	defer cancel()

	mod.log.Log("taking out hole %v out of pool, starting sync with %v",
		holeNonce, q.Caller())

	// Receive lock
	err = ch.Switch(
		nat.ExpectConsumeHoleSignal(holeNonce, nat.ConsumeHoleSignalTypeLock, nil),
		channel.PassErrors,
		channel.WithContext(opCtx),
	)
	if err != nil {
		return ch.Send(astral.Err(err))
	}

	if !hole.BeginLock() {
		_ = ch.Send(&nat.ConsumeHoleSignal{Signal: nat.ConsumeHoleSignalTypeLocked, Pair: holeNonce, Ok: false, Error: astral.String8(natmod.ErrHoleBusy.Error())})
		return ch.Send(astral.Err(natmod.ErrHoleBusy))
	}

	if err := hole.WaitLocked(opCtx); err != nil {
		return ch.Send(astral.Err(err))
	}
	if err := ch.Send(&nat.ConsumeHoleSignal{Signal: nat.ConsumeHoleSignalTypeLocked, Pair: holeNonce, Ok: true}); err != nil {
		return ch.Send(astral.Err(err))
	}

	// Receive take
	err = ch.Switch(
		nat.ExpectConsumeHoleSignal(holeNonce, nat.ConsumeHoleSignalTypeTake, nil),
		channel.PassErrors,
		channel.WithContext(opCtx),
	)
	if err != nil {
		return ch.Send(astral.Err(err))
	}

	if err := ch.Send(&nat.ConsumeHoleSignal{Signal: nat.ConsumeHoleSignalTypeTaken, Pair: holeNonce, Ok: true}); err != nil {
		return ch.Send(astral.Err(err))
	}

	return ch.Send(&hole.Hole)
}
