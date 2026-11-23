package nat

import (
	"context"
	"errors"
	"net"
	"testing"
	"time"

	"github.com/astralp2p/astral-go/api/ip"
	"github.com/astralp2p/astral-go/api/nat"
	"github.com/astralp2p/astral-go/astral"
	"github.com/astralp2p/astral-go/astral/log"
	"github.com/astralp2p/astrald/mod/dir"
	natmod "github.com/astralp2p/astrald/mod/nat"
)

// peerHandshakeDelay outlasts consumePeerNoPing, the peer hole's no-ping timeout.
const (
	consumePeerNoPing   = 300 * time.Millisecond
	peerHandshakeDelay  = 1 * time.Second
	consumeAnswerWithin = 10 * time.Second
)

// oneIdentityDir resolves every name to one identity.
type oneIdentityDir struct {
	dir.Module
	id *astral.Identity
}

func (d *oneIdentityDir) ResolveIdentity(string) (*astral.Identity, error) { return d.id, nil }

// consumeFixture is an initiator module whose pool holds the pinger side of a
// hole. The peer holds the responder side and has no other link to the initiator.
type consumeFixture struct {
	mod      *Module
	local    *Hole
	peerHole *Hole
	peer     *astral.Identity
}

func newConsumeFixture(t *testing.T) *consumeFixture {
	t.Helper()
	self, peer := astral.GenerateIdentity(), astral.GenerateIdentity()
	shape := nat.Hole{
		Nonce:           astral.NewNonce(),
		ActiveIdentity:  self,
		ActiveEndpoint:  nat.Endpoint{IP: ip.IP(net.ParseIP("10.0.0.1").To4()), Port: 40000},
		PassiveIdentity: peer,
		PassiveEndpoint: nat.Endpoint{IP: ip.IP(net.ParseIP("10.0.0.2").To4()), Port: 40001},
	}
	aConn, bConn := NewMemPacketPair()
	tick := WithPingInterval(50 * time.Millisecond)
	f := &consumeFixture{
		local:    NewHoleWithConn(shape, self, true, aConn, tick, WithPingLifespan(200*time.Millisecond)),
		peerHole: NewHoleWithConn(shape, peer, false, bConn, tick, WithNoPingTimeout(consumePeerNoPing)),
		peer:     peer,
	}

	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	if err := f.local.StartKeepAlive(ctx); err != nil {
		t.Fatalf("start local keepalive: %v", err)
	}
	if err := f.peerHole.StartKeepAlive(ctx); err != nil {
		t.Fatalf("start peer keepalive: %v", err)
	}

	f.mod = &Module{
		Deps: Deps{Auth: &recordingAuth{verdict: true}, Dir: &oneIdentityDir{id: peer}},
		log:  log.New(nil),
	}
	f.mod.pool = NewHolePool(f.mod)
	if err := f.mod.pool.Add(f.local); err != nil {
		t.Fatalf("add hole: %v", err)
	}
	return f
}

// lockPeer is the responder's lock step: a hole gone from its keepalive answers
// as the responder does when its pool no longer holds the hole.
func (f *consumeFixture) lockPeer(ctx *astral.Context) error {
	if !f.peerHole.BeginLock() {
		return natmod.ErrHoleNotExists
	}
	return f.peerHole.WaitLocked(ctx)
}

// consume routes nat.node_consume_hole as the initiator and returns its answer.
func (f *consumeFixture) consume(t *testing.T) astral.Object {
	t.Helper()
	w := newRecordingWriter()
	q := "nat.node_consume_hole?pair=" + f.local.Nonce.String() + "&identity=" + f.peer.String()
	if err := route(t, f.mod.OpNodeConsumeHole, astral.GenerateIdentity(), q, w); err != nil {
		t.Fatalf("route: %v", err)
	}
	select {
	case <-w.closed:
	case <-time.After(consumeAnswerWithin):
		t.Fatal("the op never closed the connection")
	}
	objs := w.objects(t)
	if len(objs) != 1 {
		t.Fatalf("the op answered %v; want one object", objs)
	}
	return objs[0]
}

// TestOpNodeConsumeHole_InitiatorLocksAfterPeer holds that the initiator keeps
// pinging while the peer's handshake is slower than the peer's no-ping timeout,
// so the peer's hole survives until it locks and the consume completes.
func TestOpNodeConsumeHole_InitiatorLocksAfterPeer(t *testing.T) {
	f := newConsumeFixture(t)
	var localDuring HoleState
	var peerExpired bool
	f.mod.consumeRemote = func(ctx *astral.Context, _ *astral.Identity, _ astral.Nonce) error {
		time.Sleep(peerHandshakeDelay)
		localDuring, peerExpired = f.local.State(), f.peerHole.IsExpired()
		return f.lockPeer(ctx)
	}

	answer := f.consume(t)

	if localDuring != StateIdle {
		t.Errorf("local hole state %v during the peer handshake; want StateIdle (%v)", localDuring, StateIdle)
	}
	if peerExpired {
		t.Errorf("peer hole expired before its lock signal arrived")
	}
	if answer.ObjectType() != "ack" {
		t.Fatalf("the op answered %v; want ack", answer)
	}
	if !f.local.IsLocked() || !f.peerHole.IsLocked() {
		t.Fatalf("local %v, peer %v; want both StateLocked (%v)", f.local.State(), f.peerHole.State(), StateLocked)
	}
}

// TestOpNodeConsumeHole_RemoteFailureReturnsHoleToPool holds that a failed peer
// handshake leaves the initiator's hole Idle and back in the pool.
func TestOpNodeConsumeHole_RemoteFailureReturnsHoleToPool(t *testing.T) {
	f := newConsumeFixture(t)
	errPeer := errors.New("peer unreachable")
	f.mod.consumeRemote = func(*astral.Context, *astral.Identity, astral.Nonce) error { return errPeer }

	answer := f.consume(t)

	if msg, ok := answer.(*astral.ErrorMessage); !ok || msg.Error() != errPeer.Error() {
		t.Fatalf("the op answered %v; want error_message %q", answer, errPeer)
	}
	h, ok := f.mod.pool.Get(f.local.Nonce)
	if !ok || h != f.local || !h.IsIdle() {
		t.Fatalf("pool holds %v (found %v); want the Idle local hole", h, ok)
	}
}

// TestOpNodeConsumeHole_LocalExpiredDuringHandshake holds that a local hole
// gone during the peer handshake answers that the hole cannot lock.
func TestOpNodeConsumeHole_LocalExpiredDuringHandshake(t *testing.T) {
	f := newConsumeFixture(t)
	f.mod.consumeRemote = func(ctx *astral.Context, _ *astral.Identity, _ astral.Nonce) error {
		f.local.Expire()
		return f.lockPeer(ctx)
	}

	answer := f.consume(t)

	if msg, ok := answer.(*astral.ErrorMessage); !ok || msg.Error() != natmod.ErrHoleCantLock.Error() {
		t.Fatalf("the op answered %v; want error_message %q", answer, natmod.ErrHoleCantLock)
	}
	if _, ok := f.mod.pool.Get(f.local.Nonce); ok {
		t.Fatal("the pool holds the expired hole")
	}
}
