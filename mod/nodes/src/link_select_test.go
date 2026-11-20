package nodes

import (
	"errors"
	"testing"
	"time"

	"github.com/astralp2p/astral-go/astral"
	"github.com/astralp2p/astral-go/astral/channel"
)

var errTestDead = errors.New("test: link dead")

// fixedPressure is a pressure detector whose state a test sets.
type fixedPressure struct{ high bool }

func (p fixedPressure) OnBytes(int, time.Time)         {}
func (p fixedPressure) OnRTT(time.Duration, time.Time) {}
func (p fixedPressure) IsHigh() bool                   { return p.high }

// selectLinkOpt shapes a link built by newSelectable.
type selectLinkOpt func(*Link)

// aged sets createdAt to age before now.
func aged(age time.Duration) selectLinkOpt {
	return func(l *Link) { l.createdAt = time.Now().Add(-age) }
}

// pinging marks a ping in flight, as beginPing does.
func pinging() selectLinkOpt {
	return func(l *Link) { l.ping = &Ping{nonce: astral.NewNonce(), sentAt: time.Now(), pong: make(chan struct{})} }
}

// idleExpired makes the link's probe due: it heard nothing for longer than its idle ping.
func idleExpired() selectLinkOpt {
	return func(l *Link) { l.lastRecv.Store(time.Now().Add(-2 * l.idlePing).UnixNano()) }
}

// pressured sets the link's pressure detector state.
func pressured(high bool) selectLinkOpt {
	return func(l *Link) { l.pressure = fixedPressure{high: high} }
}

// newSelectable builds a link to remote that no loop drives, so its state is only what opts set.
// Its idle ping is one hour and it just received a frame, so by default it is not suspect.
func newSelectable(t *testing.T, mod *Module, remote *astral.Identity, opts ...selectLinkOpt) *Link {
	t.Helper()
	l := &Link{
		id:        astral.NewNonce(),
		conn:      &identityConn{remote: remote},
		mux:       newMux(mod, channel.New(&recordingLink{}, channel.WithLockedWrites()), mod.node.Identity(), remote, nil, nil),
		done:      make(chan struct{}),
		createdAt: time.Now(),
		idlePing:  time.Hour,
	}
	l.lastRecv.Store(time.Now().UnixNano())
	for _, opt := range opts {
		opt(l)
	}
	t.Cleanup(func() { close(l.done) })
	if err := mod.linkPool.links.Add(l); err != nil {
		t.Fatalf("pool.links.Add: %v", err)
	}
	return l
}

func newSelectionPool() (*Module, *astral.Identity) {
	localID, remoteID := astral.GenerateIdentity(), astral.GenerateIdentity()
	mod := &Module{node: &identityNode{id: localID}}
	mod.linkPool = NewLinkPool(mod)
	return mod, remoteID
}

// TestSelectSkipsLinkWithPingInFlight: of two links to one identity, the older has a ping in flight; the newer is returned.
func TestSelectSkipsLinkWithPingInFlight(t *testing.T) {
	// why both orders: neither the suspect state nor the age alone explains the pick.
	for _, c := range []struct {
		name           string
		suspect, other time.Duration
	}{
		{"suspect older", time.Minute, time.Second},
		{"suspect newer", time.Second, time.Minute},
	} {
		t.Run(c.name, func(t *testing.T) {
			mod, remote := newSelectionPool()
			suspect := newSelectable(t, mod, remote, aged(c.suspect), pinging())
			healthy := newSelectable(t, mod, remote, aged(c.other))
			if got := mod.linkPool.SelectLinkWith(remote); got != healthy {
				t.Fatalf("SelectLinkWith returned %v (suspect %v, healthy %v); want the link without a ping in flight", got.id, suspect.id, healthy.id)
			}
		})
	}
}

// TestSelectPrefersNewestWhenHealthy: of links neither suspect nor under pressure, the newest createdAt is returned, whatever the pool order.
func TestSelectPrefersNewestWhenHealthy(t *testing.T) {
	mod, remote := newSelectionPool()
	oldest := newSelectable(t, mod, remote, aged(3*time.Minute))
	newest := newSelectable(t, mod, remote, aged(time.Second))
	middle := newSelectable(t, mod, remote, aged(time.Minute))

	if got := mod.linkPool.SelectLinkWith(remote); got != newest {
		t.Fatalf("SelectLinkWith returned %v; want the newest %v (oldest %v, middle %v)", got.id, newest.id, oldest.id, middle.id)
	}
}

// TestSelectEqualAgeKeepsPoolOrder: links with equal keys resolve to the one first in the pool.
func TestSelectEqualAgeKeepsPoolOrder(t *testing.T) {
	mod, remote := newSelectionPool()
	at := time.Now()
	first := newSelectable(t, mod, remote, func(l *Link) { l.createdAt = at })
	newSelectable(t, mod, remote, func(l *Link) { l.createdAt = at })

	if got := mod.linkPool.SelectLinkWith(remote); got != first {
		t.Fatalf("SelectLinkWith returned %v; want the first pooled link %v", got.id, first.id)
	}
}

// TestSelectAllSuspectStillReturnsLink: when every link is suspect, one is returned, the newest of them, never nil.
func TestSelectAllSuspectStillReturnsLink(t *testing.T) {
	mod, remote := newSelectionPool()
	older := newSelectable(t, mod, remote, aged(time.Minute), pinging())
	newer := newSelectable(t, mod, remote, aged(time.Second), idleExpired())

	got := mod.linkPool.SelectLinkWith(remote)
	if got == nil {
		t.Fatal("SelectLinkWith returned nil with every link suspect")
	}
	if got != newer {
		t.Fatalf("SelectLinkWith returned %v; want the newest suspect link %v (older %v)", got.id, newer.id, older.id)
	}
}

// TestSuspectWhenIdleLeftExpired: a link whose idle deadline passed with no ping in flight is suspect and is passed over for a non-suspect sibling, though it is the newest.
func TestSuspectWhenIdleLeftExpired(t *testing.T) {
	mod, remote := newSelectionPool()
	expired := newSelectable(t, mod, remote, aged(time.Second), idleExpired())
	healthy := newSelectable(t, mod, remote, aged(time.Minute))

	if expired.idleLeft() > 0 {
		t.Fatalf("idleLeft is %v; want at most 0", expired.idleLeft())
	}
	if expired.ping != nil {
		t.Fatal("the expired link has a ping in flight; want none")
	}
	if !expired.Suspect() {
		t.Fatal("Suspect() is false for a link past its idle deadline")
	}
	if healthy.Suspect() {
		t.Fatal("Suspect() is true for a link that just received a frame")
	}
	if got := mod.linkPool.SelectLinkWith(remote); got != healthy {
		t.Fatalf("SelectLinkWith returned %v; want the non-suspect sibling %v", got.id, healthy.id)
	}
}

// TestSuspectFollowsPing: a real beginPing makes the link suspect; the pong's removal of the ping clears it.
func TestSuspectFollowsPing(t *testing.T) {
	mod := &Module{}
	remote := astral.GenerateIdentity()
	link, _ := newPeerLink(t, mod, remote, time.Hour, time.Hour)
	if link.Suspect() {
		t.Fatal("a new link is suspect")
	}
	p, _ := link.beginPing()
	if !link.Suspect() {
		t.Fatal("a link with a ping in flight is not suspect")
	}
	link.pingMu.Lock()
	if link.ping != p {
		link.pingMu.Unlock()
		t.Fatal("beginPing did not record the ping")
	}
	link.ping = nil
	link.pingMu.Unlock()
	if link.Suspect() {
		t.Fatal("a link without a ping in flight and inside its idle deadline is suspect")
	}
}

// TestSelectHealthyPressureBeatsSuspectNoPressure: a non-suspect link under high pressure is chosen over a suspect link without pressure.
func TestSelectHealthyPressureBeatsSuspectNoPressure(t *testing.T) {
	mod, remote := newSelectionPool()
	suspect := newSelectable(t, mod, remote, aged(time.Second), pinging(), pressured(false))
	loaded := newSelectable(t, mod, remote, aged(time.Minute), pressured(true))

	if got := mod.linkPool.SelectLinkWith(remote); got != loaded {
		t.Fatalf("SelectLinkWith returned %v; want the non-suspect link under pressure %v (suspect %v)", got.id, loaded.id, suspect.id)
	}
}

// TestSelectPressureBeatsAgeAmongHealthy: of non-suspect links, one without high pressure wins over a newer one under it.
func TestSelectPressureBeatsAgeAmongHealthy(t *testing.T) {
	mod, remote := newSelectionPool()
	calm := newSelectable(t, mod, remote, aged(time.Minute), pressured(false))
	loaded := newSelectable(t, mod, remote, aged(time.Second), pressured(true))

	if got := mod.linkPool.SelectLinkWith(remote); got != calm {
		t.Fatalf("SelectLinkWith returned %v; want the link without pressure %v (loaded %v)", got.id, calm.id, loaded.id)
	}
}

// TestSelectSuspectGroupsOrderByPressure: of suspect links, one without high pressure wins; of links all suspect and under pressure, the newest.
func TestSelectSuspectGroupsOrderByPressure(t *testing.T) {
	mod, remote := newSelectionPool()
	calm := newSelectable(t, mod, remote, aged(time.Minute), pinging(), pressured(false))
	newSelectable(t, mod, remote, aged(time.Second), pinging(), pressured(true))
	if got := mod.linkPool.SelectLinkWith(remote); got != calm {
		t.Fatalf("SelectLinkWith returned %v; want the suspect link without pressure %v", got.id, calm.id)
	}

	mod, remote = newSelectionPool()
	newSelectable(t, mod, remote, aged(time.Minute), pinging(), pressured(true))
	newest := newSelectable(t, mod, remote, aged(time.Second), pinging(), pressured(true))
	if got := mod.linkPool.SelectLinkWith(remote); got != newest {
		t.Fatalf("SelectLinkWith returned %v; want the newest of the suspect links under pressure %v", got.id, newest.id)
	}
}

// TestSelectNeverReturnsErroredLink: a link with Err() != nil is not returned, even as the newest, and nil returns when only errored links remain.
func TestSelectNeverReturnsErroredLink(t *testing.T) {
	mod, remote := newSelectionPool()
	live := newSelectable(t, mod, remote, aged(time.Minute), pinging())
	dead := newSelectable(t, mod, remote, aged(time.Second))
	dead.err.Set(errTestDead)

	if got := mod.linkPool.SelectLinkWith(remote); got != live {
		t.Fatalf("SelectLinkWith returned %v; want the open link %v, not the errored newest %v", got.id, live.id, dead.id)
	}

	live.err.Set(errTestDead)
	if got := mod.linkPool.SelectLinkWith(remote); got != nil {
		t.Fatalf("SelectLinkWith returned %v with only errored links; want nil", got.id)
	}
}

// TestSelectIgnoresOtherPeers: a newer link to another identity is never returned.
func TestSelectIgnoresOtherPeers(t *testing.T) {
	mod, remote := newSelectionPool()
	mine := newSelectable(t, mod, remote, aged(time.Minute), pinging())
	newSelectable(t, mod, astral.GenerateIdentity(), aged(time.Second))

	if got := mod.linkPool.SelectLinkWith(remote); got != mine {
		t.Fatalf("SelectLinkWith returned %v; want the link to the asked identity %v", got.id, mine.id)
	}
}
