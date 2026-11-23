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
	ch := channel.New(q.AcceptRaw(), channel.WithFormats(args.In, args.Out))
	defer ch.Close()

	if !mod.Auth.Authorize(ctx, &auth.AdminNetworkAction{Action: auth.NewAction(q.Caller())}) {
		return ch.Send(auth.ErrNotPermitted)
	}

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

		// why: this node is the hole's only pinger, and locking silences its pings.
		// The peer expires an Idle hole noPingTimeout after the last ping it heard,
		// so a lock taken before the peer holds the signal lets signaling latency
		// (a stalled peer or transport) expire the peer's hole. The hole stays Idle
		// until the peer's handshake completes, in the order
		// protocols/nat/ops/nat.node_consume_hole.md gives: open on the peer, then lock.
		// A failed handshake leaves the hole Idle, so the defer returns it to the pool.
		if err := mod.consumeRemoteHole(opCtx, target, holeNonce); err != nil {
			return ch.Send(astral.Err(err))
		}

		// note: the hole can expire or leave StateIdle during the handshake; the
		// peer's side is taken by then and nothing gives it back.
		if !hole.BeginLock() {
			return ch.Send(astral.Err(natmod.ErrHoleCantLock))
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

// consumeRemoteHole opens nat.node_consume_hole for pair on peer and drives the
// peer's lock-then-take handshake to its end.
func (mod *Module) consumeRemoteHole(ctx *astral.Context, peer *astral.Identity, pair astral.Nonce) error {
	if mod.consumeRemote != nil {
		return mod.consumeRemote(ctx, peer, pair)
	}

	// why: the peer is asked as this node; NAT traversal runs between nodes, and
	// mod.auth.admin_network_action in OpNodeConsumeHole decides who starts it (decided 2026-11-06).
	return natclient.New(peer, astrald.Default()).NodeConsumeHole(ctx, pair, nil)
}
