package services

import (
	"errors"
	"io"
	"testing"
	"time"

	"github.com/astralp2p/astral-go/api/services"
	"github.com/astralp2p/astral-go/astral"
	"github.com/astralp2p/astral-go/astral/channel"
	"github.com/astralp2p/astral-go/lib/query"
	"github.com/astralp2p/astral-go/lib/routing"
	"github.com/astralp2p/astrald/mod/services/src/coordinator"
)

// Wire tests: a provider and a consumer talk to the real ops over channels,
// with the coordinator behind them.

const testWait = 5 * time.Second

func wireModule(verdict bool) (*Module, *recordingAuth) {
	authority := &recordingAuth{verdict: verdict}
	cfg := coordinator.DefaultConfig()
	cfg.InitialBudget = time.Second
	return &Module{
		Deps:  Deps{Auth: authority},
		node:  &serveAppsNode{id: astral.GenerateIdentity()},
		coord: coordinator.New(cfg),
	}, authority
}

// peer is the caller's end of one query.
type peer struct {
	ch   *channel.Channel
	conn io.WriteCloser
	in   *io.PipeReader
}

func (p *peer) close() {
	p.conn.Close()
	p.in.Close()
}

type pipeConn struct {
	io.Reader
	io.Writer
}

func open(t *testing.T, fn any, caller *astral.Identity, queryString string, network bool) *peer {
	t.Helper()

	op, err := routing.NewOp(fn)
	if err != nil {
		t.Fatalf("new op: %v", err)
	}

	ctx, cancel := astral.NewContext(nil).WithCancel()
	t.Cleanup(cancel)

	q := astral.Launch(query.New(caller, caller, queryString, nil))
	if network {
		q.Extra.Set("origin", astral.OriginNetwork)
	}

	in, out := io.Pipe()
	conn, err := op.RouteQuery(ctx, q, out)
	if err != nil {
		t.Fatalf("%s: %v", queryString, err)
	}

	p := &peer{ch: channel.New(pipeConn{in, conn}, channel.WithLockedWrites()), conn: conn, in: in}
	t.Cleanup(p.close)
	return p
}

type received struct {
	obj astral.Object
	err error
}

// recv returns the next object from p, or the read error.
func recv(t *testing.T, p *peer) (astral.Object, error) {
	t.Helper()
	got := make(chan received, 1)
	go func() {
		obj, err := p.ch.Receive()
		got <- received{obj, err}
	}()
	select {
	case r := <-got:
		return r.obj, r.err
	case <-time.After(testWait):
		t.Fatal("nothing received")
		return nil, nil
	}
}

func expect[T astral.Object](t *testing.T, p *peer) T {
	t.Helper()
	obj, err := recv(t, p)
	if err != nil {
		t.Fatalf("receive: %v", err)
	}
	v, ok := obj.(T)
	if !ok {
		var zero T
		t.Fatalf("received %s %v; want %T", obj.ObjectType(), obj, zero)
	}
	return v
}

func expectEnd(t *testing.T, p *peer) {
	t.Helper()
	obj, err := recv(t, p)
	if !errors.Is(err, io.EOF) {
		t.Fatalf("received %v, %v; want the channel closed", obj, err)
	}
}

// advertise binds a provider that answers every ask with offer.
func advertise(t *testing.T, mod *Module, list string, offer func(*services.Ask) *services.Update) (*peer, *astral.Identity) {
	t.Helper()
	id := astral.GenerateIdentity()
	p := open(t, mod.OpAdvertise, id, "services.advertise?services="+list, false)
	expect[*astral.Ack](t, p)

	go p.ch.Switch(func(ask *services.Ask) error {
		u := offer(ask)
		u.Name = ask.Service
		return p.ch.Send(&services.Answer{RequestID: ask.RequestID, Update: u})
	})
	return p, id
}

func available(*services.Ask) *services.Update { return &services.Update{Available: true} }

func TestDiscoverOnceShowsTheProviderAndCloses(t *testing.T) {
	mod, _ := wireModule(true)
	_, providerID := advertise(t, mod, "player", available)

	c := open(t, mod.OpDiscover, astral.GenerateIdentity(), "services.discover?services=player", false)

	u := expect[*services.Update](t, c)
	if string(u.Name) != "player" || !bool(u.Available) || !u.ProviderID.IsEqual(providerID) {
		t.Fatalf("got %+v; want player available from the provider", u)
	}
	expect[*astral.EOS](t, c)
	expectEnd(t, c)
}

func TestDiscoverFollowSeesChangeThenRemoval(t *testing.T) {
	mod, _ := wireModule(true)
	p, providerID := advertise(t, mod, "player", available)
	caller := astral.GenerateIdentity()

	c := open(t, mod.OpDiscover, caller, "services.discover?services=player&follow=true", false)
	expect[*services.Update](t, c)
	expect[*astral.EOS](t, c)

	if err := p.ch.Send(&services.Change{Callers: []*astral.Identity{caller}}); err != nil {
		t.Fatalf("send change: %v", err)
	}
	expect[*services.Update](t, c)

	p.close()
	r := expect[*services.Removed](t, c)
	if len(r.Offerings) != 1 || !r.Offerings[0].ProviderID.IsEqual(providerID) || r.Offerings[0].Name != "player" {
		t.Fatalf("removed %+v; want the provider's player offering", r.Offerings)
	}
}

func TestDiscoverReportsAProviderThatDoesNotAnswer(t *testing.T) {
	mod, _ := wireModule(true)
	id := astral.GenerateIdentity()
	p := open(t, mod.OpAdvertise, id, "services.advertise?services=player", false)
	expect[*astral.Ack](t, p)

	c := open(t, mod.OpDiscover, astral.GenerateIdentity(), "services.discover?services=player", false)
	in := expect[*services.Incomplete](t, c)
	if len(in.Services) != 1 || in.Services[0] != "player" {
		t.Fatalf("incomplete %+v; want player", in.Services)
	}
	expect[*astral.EOS](t, c)
	expectEnd(t, c)
}

func TestDiscoverOfAnUnregisteredServiceEndsAtOnce(t *testing.T) {
	mod, _ := wireModule(true)
	c := open(t, mod.OpDiscover, astral.GenerateIdentity(), "services.discover?services=nothing", false)
	expect[*astral.EOS](t, c)
	expectEnd(t, c)
}

// TestDiscoverRefusesBeforeEvaluating holds the admission rule: one service the
// caller may not discover refuses the whole request, and no provider is asked.
func TestDiscoverRefusesBeforeEvaluating(t *testing.T) {
	mod, authority := wireModule(true)
	asked := make(chan struct{}, 1)
	advertise(t, mod, "player", func(*services.Ask) *services.Update {
		asked <- struct{}{}
		return &services.Update{Available: true}
	})
	authority.mu.Lock()
	authority.verdict = false
	authority.actions = nil
	authority.mu.Unlock()
	caller := astral.GenerateIdentity()

	c := open(t, mod.OpDiscover, caller, "services.discover?services=player,wallet", false)
	expect[*astral.ErrorMessage](t, c)

	var actions []*services.ServiceDiscoveryAction
	for _, a := range authority.recorded() {
		if d, ok := a.(*services.ServiceDiscoveryAction); ok {
			actions = append(actions, d)
		}
	}
	if len(actions) != 1 {
		t.Fatalf("made %d discovery checks; want 1, stopping at the first refusal", len(actions))
	}
	a := actions[0]
	if !a.Actor().IsEqual(caller) || a.Service != "player" || !a.NodeID.IsEqual(mod.node.Identity()) {
		t.Fatalf("checked %+v; want the caller discovering player on this node", a)
	}

	select {
	case <-asked:
		t.Fatal("a refused discovery asked the provider")
	case <-time.After(50 * time.Millisecond):
	}
}

func TestDiscoverRejectsAnInvalidList(t *testing.T) {
	for _, list := range []string{"", "a,a", "a,,b"} {
		t.Run(list, func(t *testing.T) {
			mod, authority := wireModule(true)
			c := open(t, mod.OpDiscover, astral.GenerateIdentity(), "services.discover?services="+list, false)
			expect[*astral.ErrorMessage](t, c)
			if n := len(authority.recorded()); n != 0 {
				t.Fatalf("made %d authorization calls for an invalid list; want none", n)
			}
		})
	}
}

// TestDiscoverServesALinkCaller holds the 2026-10-01 decision: a query arriving
// over a link is checked for the identity that sent it and answered from local
// providers.
func TestDiscoverServesALinkCaller(t *testing.T) {
	mod, authority := wireModule(true)
	_, providerID := advertise(t, mod, "player", available)
	remote := astral.GenerateIdentity()

	c := open(t, mod.OpDiscover, remote, "services.discover?services=player", true)
	u := expect[*services.Update](t, c)
	if !u.ProviderID.IsEqual(providerID) {
		t.Fatalf("got %+v; want the local provider's offering", u)
	}
	expect[*astral.EOS](t, c)

	for _, a := range authority.recorded() {
		if d, ok := a.(*services.ServiceDiscoveryAction); ok && !d.Actor().IsEqual(remote) {
			t.Fatalf("checked discovery for %v; want the remote caller %v", d.Actor(), remote)
		}
	}
}

func TestAdvertiseRefusesAnOwnedSet(t *testing.T) {
	mod, _ := wireModule(true)
	_, id := advertise(t, mod, "player", available)

	p := open(t, mod.OpAdvertise, id, "services.advertise?services=wallet,player", false)
	expect[*astral.ErrorMessage](t, p)
	expectEnd(t, p)

	if owned(mod, id, "wallet") {
		t.Fatal("a refused advertisement claimed part of its set")
	}
}

func TestAdvertiseRejectsAnInvalidList(t *testing.T) {
	mod, _ := wireModule(true)
	p := open(t, mod.OpAdvertise, astral.GenerateIdentity(), "services.advertise?services=a,a", false)
	expect[*astral.ErrorMessage](t, p)
	expectEnd(t, p)
}

// TestAdvertiseEndsOnAnUnexpectedObject holds the binding rule: any object other
// than an answer or a change ends the binding and releases its names.
func TestAdvertiseEndsOnAnUnexpectedObject(t *testing.T) {
	mod, _ := wireModule(true)
	id := astral.GenerateIdentity()
	p := open(t, mod.OpAdvertise, id, "services.advertise?services=player", false)
	expect[*astral.Ack](t, p)

	if err := p.ch.Send(astral.NewString8("hello")); err != nil {
		t.Fatalf("send: %v", err)
	}
	expectEnd(t, p)

	deadline := time.Now().Add(testWait)
	for owned(mod, id, "player") {
		if time.Now().After(deadline) {
			t.Fatal("the ended binding still holds its names")
		}
		time.Sleep(time.Millisecond)
	}
}

type staticEvaluator struct {
	available bool
}

func (e *staticEvaluator) Evaluate(*astral.Identity, string) *services.Update {
	return &services.Update{Available: astral.Bool(e.available)}
}

func TestNativeServiceAnswersAsTheNode(t *testing.T) {
	mod, _ := wireModule(true)
	if _, err := mod.RegisterNative([]string{"nat"}, &staticEvaluator{available: true}); err != nil {
		t.Fatalf("register: %v", err)
	}

	c := open(t, mod.OpDiscover, astral.GenerateIdentity(), "services.discover?services=nat", false)
	u := expect[*services.Update](t, c)
	if string(u.Name) != "nat" || !bool(u.Available) || !u.ProviderID.IsEqual(mod.node.Identity()) {
		t.Fatalf("got %+v; want nat available from the node", u)
	}
	expect[*astral.EOS](t, c)
}

func TestNativeChangedReachesAFollower(t *testing.T) {
	mod, _ := wireModule(true)
	eval := &staticEvaluator{available: true}
	reg, err := mod.RegisterNative([]string{"nat"}, eval)
	if err != nil {
		t.Fatalf("register: %v", err)
	}

	c := open(t, mod.OpDiscover, astral.GenerateIdentity(), "services.discover?services=nat&follow=true", false)
	expect[*services.Update](t, c)
	expect[*astral.EOS](t, c)

	reg.Changed()
	if u := expect[*services.Update](t, c); !bool(u.Available) {
		t.Fatalf("got %+v; want a fresh available view", u)
	}

	reg.Close()
	expect[*services.Removed](t, c)
}

func TestNativeRegistrationIsExclusive(t *testing.T) {
	mod, _ := wireModule(true)
	if _, err := mod.RegisterNative([]string{"nat"}, &staticEvaluator{}); err != nil {
		t.Fatalf("register: %v", err)
	}
	if _, err := mod.RegisterNative([]string{"nat"}, &staticEvaluator{}); !errors.Is(err, coordinator.ErrOwned) {
		t.Fatalf("second registration: got %v; want ErrOwned", err)
	}
}
