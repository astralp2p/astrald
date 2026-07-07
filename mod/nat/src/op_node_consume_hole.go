package nat

import (
	natmod "github.com/cryptopunkscc/astrald/mod/nat"
	"time"

	"github.com/cryptopunkscc/astral-go/api/nat"
	"github.com/cryptopunkscc/astral-go/astral"
	"github.com/cryptopunkscc/astral-go/astral/channel"
	"github.com/cryptopunkscc/astral-go/lib/astrald"
	"github.com/cryptopunkscc/astral-go/lib/routing"
	natclient "github.com/cryptopunkscc/astrald/mod/nat/client"
)

const takeExchangeTimeout = 5 * time.Second

type opNodeConsumeHoleArgs struct {
	Pair   astral.Nonce
	Target string `query:"optional"`

	In  string `query:"optional"`
	Out string `query:"optional"`
}

// OpNodeConsumeHole coordinates a two-phase lock-then-take exchange to hand a hole out of the pool.
// When Target is set it acts as the initiator; otherwise it is the responder waiting for the lock signal.
func (mod *Module) OpNodeConsumeHole(ctx *astral.Context, q *routing.IncomingQuery, args opNodeConsumeHoleArgs) (err error) {
	ch := channel.New(q.AcceptRaw(), channel.WithFormats(args.In, args.Out))
	defer ch.Close()

	hole, err := mod.pool.Take(args.Pair)
	if err != nil {
		return ch.Send(astral.Err(err))
	}

	holeNonce := hole.Nonce

	if args.Target != "" {
		target, err := mod.Dir.ResolveIdentity(args.Target)
		if err != nil {
			return ch.Send(astral.Err(err))
		}

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
