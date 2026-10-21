package nodes

import (
	"errors"
	"io"
	"testing"
	"time"

	"github.com/astralp2p/astral-go/api/nodes"
	"github.com/astralp2p/astral-go/astral"
	"github.com/astralp2p/astral-go/astral/channel"
	"github.com/astralp2p/astral-go/astral/log"
)

// migrationFixture is a module with one open mux session on oldLink and an
// empty targetLink to migrate it to.
type migrationFixture struct {
	mod        *Module
	session    *session
	oldLink    *Link
	targetLink *Link
}

func newMigrationFixture(t *testing.T) *migrationFixture {
	t.Helper()

	localID, remoteID := astral.GenerateIdentity(), astral.GenerateIdentity()
	mod := &Module{log: log.New(localID)}
	mod.linkPool = NewLinkPool(mod)

	newLink := func() *Link {
		ch := channel.New(&recordingLink{}, channel.WithLockedWrites())
		return &Link{id: astral.NewNonce(), mux: newMux(mod, ch, localID, remoteID, nil, nil)}
	}
	oldLink, targetLink := newLink(), newLink()
	if err := mod.linkPool.links.Add(oldLink, targetLink); err != nil {
		t.Fatalf("add links: %v", err)
	}

	sess, ok := oldLink.mux.createSession(astral.NewNonce(), remoteID, remoteID, "test", true, defaultBufferSize)
	if !ok {
		t.Fatal("create session")
	}
	sess.setState(stateOpen)

	// why: the old input buffer closes when the peer's migrate frame arrives.
	// Closed upfront, WaitClosed returns at once, so a signal read that wrongly
	// succeeds runs the migration to completion instead of timing out.
	sess.reader.(*muxSessionReader).Buf().Close()

	return &migrationFixture{mod: mod, session: sess, oldLink: oldLink, targetLink: targetLink}
}

// migrated reports whether the session now lives on the target link's mux.
func (f *migrationFixture) migrated() bool {
	_, ok := f.targetLink.mux.sessions.Get(f.session.Nonce)
	return ok
}

// migrationPeer is the far end of the signalling channel. It receives every
// object the local side sends, and closing its write side gives the local
// side EOF while local sends still succeed.
type migrationPeer struct {
	out      *channel.Channel
	outClose io.Closer
	received chan astral.Object
}

// newMigrationPeer answers the local side's channel and its peer.
func newMigrationPeer(t *testing.T) (*channel.Channel, *migrationPeer) {
	t.Helper()

	toLocalR, toLocalW := io.Pipe()
	toPeerR, toPeerW := io.Pipe()
	t.Cleanup(func() {
		toLocalW.Close()
		toPeerR.Close()
	})

	peer := &migrationPeer{
		out:      channel.New(channel.Join(eofReader{}, toLocalW)),
		outClose: toLocalW,
		received: make(chan astral.Object, 16),
	}
	go func() {
		recv := channel.NewReceiver(toPeerR)
		for {
			obj, err := recv.Receive()
			if err != nil {
				return
			}
			peer.received <- obj
		}
	}()

	return channel.New(channel.Join(toLocalR, toPeerW)), peer
}

// eofReader is the peer's unused read side; the peer reads through received.
type eofReader struct{}

func (eofReader) Read([]byte) (int, error) { return 0, io.EOF }

func (p *migrationPeer) expect(t *testing.T, signal astral.String8) {
	t.Helper()
	select {
	case obj := <-p.received:
		sig, ok := obj.(*nodes.MigrateSignal)
		if !ok || sig.Signal != signal {
			t.Errorf("peer received %v; want the %v signal", obj, signal)
		}
	case <-time.After(effectTimeout):
		t.Errorf("peer never received the %v signal", signal)
	}
}

func (p *migrationPeer) send(t *testing.T, signal astral.String8) {
	t.Helper()
	if err := p.out.Send(&nodes.MigrateSignal{Signal: signal, Buffer: defaultBufferSize}); err != nil {
		t.Errorf("peer send %v: %v", signal, err)
	}
}

func (p *migrationPeer) close() { p.outClose.Close() }

// TestMigrationFailsWhenPeerClosesBeforeASignal closes the peer before each of
// the four signal reads of a session migration. Each case must fail the
// migration with io.ErrUnexpectedEOF and leave the session off the target
// link, rather than read the missing signal as received.
func TestMigrationFailsWhenPeerClosesBeforeASignal(t *testing.T) {
	type side func(*migrationFixture, *astral.Context, *channel.Channel) error
	initiator := func(f *migrationFixture, ctx *astral.Context, ch *channel.Channel) error {
		return f.mod.initiateMigration(ctx, ch, f.session, f.targetLink)
	}
	responder := func(f *migrationFixture, ctx *astral.Context, ch *channel.Channel) error {
		return f.mod.acceptMigration(ctx, ch, f.session, f.targetLink)
	}

	tests := []struct {
		name string
		side side
		peer func(*testing.T, *migrationPeer)
	}{
		{"initiator before switched", initiator, func(t *testing.T, p *migrationPeer) {
			p.expect(t, nodes.MigrateSignalReady)
			p.close()
		}},
		{"initiator before done", initiator, func(t *testing.T, p *migrationPeer) {
			p.expect(t, nodes.MigrateSignalReady)
			p.send(t, nodes.MigrateSignalSwitched)
			p.expect(t, nodes.MigrateSignalResume)
			p.close()
		}},
		{"responder before ready", responder, func(t *testing.T, p *migrationPeer) {
			p.close()
		}},
		{"responder before resume", responder, func(t *testing.T, p *migrationPeer) {
			p.send(t, nodes.MigrateSignalReady)
			p.expect(t, nodes.MigrateSignalSwitched)
			p.close()
		}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f := newMigrationFixture(t)
			ch, peer := newMigrationPeer(t)

			peerDone := make(chan struct{})
			go func() {
				defer close(peerDone)
				tt.peer(t, peer)
			}()

			ctx, cancel := astral.NewContext(nil).WithTimeout(effectTimeout)
			defer cancel()

			err := tt.side(f, ctx, ch)
			<-peerDone

			if !errors.Is(err, io.ErrUnexpectedEOF) {
				t.Errorf("migration err = %v; want %v", err, io.ErrUnexpectedEOF)
			}
			if f.migrated() {
				t.Error("the session moved to the target link without the peer's signal")
			}
		})
	}
}

// TestMigrationCompletesWhenPeerSendsEverySignal is the control for the test
// above: with every signal delivered, both sides move the session to the
// target link and return nil.
func TestMigrationCompletesWhenPeerSendsEverySignal(t *testing.T) {
	t.Run("initiator", func(t *testing.T) {
		f := newMigrationFixture(t)
		ch, peer := newMigrationPeer(t)
		peerDone := make(chan struct{})
		go func() {
			defer close(peerDone)
			peer.expect(t, nodes.MigrateSignalReady)
			peer.send(t, nodes.MigrateSignalSwitched)
			peer.expect(t, nodes.MigrateSignalResume)
			peer.send(t, nodes.MigrateSignalDone)
		}()

		ctx, cancel := astral.NewContext(nil).WithTimeout(effectTimeout)
		defer cancel()
		if err := f.mod.initiateMigration(ctx, ch, f.session, f.targetLink); err != nil {
			t.Fatalf("migration err = %v; want nil", err)
		}
		<-peerDone
		if !f.migrated() {
			t.Fatal("the session is not on the target link")
		}
	})

	t.Run("responder", func(t *testing.T) {
		f := newMigrationFixture(t)
		ch, peer := newMigrationPeer(t)
		peerDone := make(chan struct{})
		go func() {
			defer close(peerDone)
			peer.send(t, nodes.MigrateSignalReady)
			peer.expect(t, nodes.MigrateSignalSwitched)
			peer.send(t, nodes.MigrateSignalResume)
			peer.expect(t, nodes.MigrateSignalDone)
		}()

		ctx, cancel := astral.NewContext(nil).WithTimeout(effectTimeout)
		defer cancel()
		if err := f.mod.acceptMigration(ctx, ch, f.session, f.targetLink); err != nil {
			t.Fatalf("migration err = %v; want nil", err)
		}
		<-peerDone
		if !f.migrated() {
			t.Fatal("the session is not on the target link")
		}
	})
}
