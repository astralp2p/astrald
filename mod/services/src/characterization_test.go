package services

import (
	"errors"
	"io"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/astralp2p/astral-go/api/auth"
	"github.com/astralp2p/astral-go/api/events"
	"github.com/astralp2p/astral-go/api/nodes"
	"github.com/astralp2p/astral-go/api/services"
	"github.com/astralp2p/astral-go/astral"
	"github.com/astralp2p/astral-go/astral/channel"
	"github.com/astralp2p/astral-go/lib/query"
	"github.com/astralp2p/astral-go/lib/routing"
	authmod "github.com/astralp2p/astrald/mod/auth"
	"github.com/astralp2p/astrald/mod/objects"
)

// Characterization tests: each pins today's behaviour of a path the readability
// refactor reshapes.

// A member refuses a carried discovery when the sending node may not discover
// the service there; the host reports the service incomplete and the member's
// provider is never asked.
func TestSwarmMemberRefusesACarryItMayNotDiscover(t *testing.T) {
	s := newSwarm()
	s.authB.refuse[services.ServiceDiscoveryAction{}.ObjectType()] = true
	asked := make(chan struct{}, 1)
	advertise(t, s.b, "player", func(*services.Ask) *services.Update {
		asked <- struct{}{}
		return &services.Update{Available: true}
	})

	c := open(t, s.a.OpDiscover, astral.GenerateIdentity(), "services.discover?services=player&reach=swarm", false)
	in := expect[*services.Incomplete](t, c)
	if len(in.Services) != 1 || in.Services[0] != "player" {
		t.Fatalf("incomplete %+v; want player", in.Services)
	}
	expect[*astral.EOS](t, c)
	select {
	case <-asked:
		t.Fatal("a refused carry asked the member's provider")
	case <-time.After(100 * time.Millisecond):
	}
}

// Ending the op's context closes a follow stream and its channel.
//
// note: routing.Op detaches the op's context, so the test calls the op
// directly with a context it can end.
func TestDiscoverEndsWhenItsContextEnds(t *testing.T) {
	mod, _ := wireModule(true)
	advertise(t, mod, "player", available)
	ctx, cancel := astral.NewContext(nil).WithCancel()
	defer cancel()
	caller := astral.GenerateIdentity()
	in, out := io.Pipe()
	defer in.Close()
	q := routing.NewIncomingQuery(query.New(caller, caller, "services.discover", nil), out, "")
	done := make(chan error, 1)
	go func() { done <- mod.OpDiscover(ctx, q, opDiscoverArgs{Services: "player", Follow: true}) }()
	p := &peer{ch: channel.New(pipeConn{Reader: in}), conn: out, in: in}

	expect[*services.Update](t, p)
	expect[*astral.EOS](t, p)
	cancel()
	select {
	case <-done:
	case <-time.After(testWait):
		t.Fatal("the op did not return")
	}
	expectEnd(t, p)
}

// stalledConn accepts no write until it is closed.
type stalledConn struct {
	io.Reader
	w *io.PipeWriter
}

func (c *stalledConn) Write(p []byte) (int, error) { return c.w.Write(p) }
func (c *stalledConn) Close() error                { return c.w.Close() }

// A write the consumer does not take within writeTimeout closes the channel
// and fails.
func TestChannelSinkClosesAStalledConsumer(t *testing.T) {
	r, w := io.Pipe()
	defer r.Close()
	sink := &channelSink{ch: channel.New(&stalledConn{Reader: strings.NewReader(""), w: w})}

	start := time.Now()
	if err := sink.Update(&services.Update{Name: "player"}); err == nil {
		t.Fatal("a stalled write succeeded")
	}
	if elapsed := time.Since(start); elapsed < writeTimeout {
		t.Fatalf("the write failed after %v; want writeTimeout %v", elapsed, writeTimeout)
	}
}

// brokenConn fails every write and counts them.
type brokenConn struct {
	io.Reader
	writes atomic.Int32
}

func (c *brokenConn) Write([]byte) (int, error) {
	c.writes.Add(1)
	return 0, errors.New("broken")
}

// A failed services.incomplete write fails the boundary before the eos.
func TestChannelSinkBoundaryStopsWhenIncompleteFails(t *testing.T) {
	conn := &brokenConn{Reader: strings.NewReader("")}
	sink := &channelSink{ch: channel.New(conn)}
	err := sink.Boundary(&services.Incomplete{Services: []astral.String8{"player"}})
	if err == nil {
		t.Fatal("the boundary succeeded over a broken channel")
	}
	if n := conn.writes.Load(); n != 1 {
		t.Fatalf("made %d writes; want 1, stopping at the incomplete", n)
	}
}

// linkDrop delivers one object as if from sender.
type linkDrop struct {
	sender *astral.Identity
	obj    astral.Object
}

func (d *linkDrop) SenderID() *astral.Identity { return d.sender }
func (d *linkDrop) Object() astral.Object      { return d.obj }
func (d *linkDrop) Accept(bool) error          { return nil }

var _ objects.Drop = &linkDrop{}

// Only a link event this node sent about its own link wakes a waiting carry.
func TestLinkEventsWakeOnlyFromThisNode(t *testing.T) {
	mod, _ := wireModule(true)
	member := astral.GenerateIdentity()
	linked := mod.links.wait(member)
	ev := &events.Event{Data: &nodes.LinkCreatedEvent{RemoteIdentity: member}}
	self := mod.node.Identity()

	for _, d := range []*linkDrop{
		{sender: astral.GenerateIdentity(), obj: ev},
		{sender: self, obj: astral.NewString8("hello")},
		{sender: self, obj: &events.Event{Data: astral.NewString8("hello")}},
	} {
		if err := mod.ReceiveObject(d); err != nil {
			t.Fatal(err)
		}
	}
	select {
	case <-linked:
		t.Fatal("a forged or unrelated object woke the carry")
	case <-time.After(50 * time.Millisecond):
	}

	if err := mod.ReceiveObject(&linkDrop{sender: self, obj: ev}); err != nil {
		t.Fatal(err)
	}
	select {
	case <-linked:
	case <-time.After(testWait):
		t.Fatal("this node's link event did not wake the carry")
	}
}

// cuttableNode routes like swarmNode and can end every stream it routed.
type cuttableNode struct {
	*swarmNode
	mu  sync.Mutex
	out []io.WriteCloser
}

func (n *cuttableNode) RouteQuery(ctx *astral.Context, q *astral.InFlightQuery, w io.WriteCloser) (io.WriteCloser, error) {
	n.mu.Lock()
	n.out = append(n.out, w)
	n.mu.Unlock()
	return n.swarmNode.RouteQuery(ctx, q, w)
}

func (n *cuttableNode) cut() {
	n.mu.Lock()
	defer n.mu.Unlock()
	for _, w := range n.out {
		_ = w.Close()
	}
	n.out = nil
}

// A follow carry retries a lost member after its backoff with no link event,
// and a carry that reached its initial outcome restarts the backoff at
// carryRetryFirst.
func TestSwarmCarryRetriesOnItsBackoff(t *testing.T) {
	s := newSwarm()
	node := &cuttableNode{swarmNode: s.nodeA}
	s.a.node = node
	_, remoteP := advertise(t, s.b, "player", available)
	s.nodeA.down.Store(true)

	c := open(t, s.a.OpDiscover, astral.GenerateIdentity(), "services.discover?services=player&reach=swarm&follow=true", false)
	expect[*services.Incomplete](t, c)
	expect[*astral.EOS](t, c)
	s.nodeA.down.Store(false)
	if u := expect[*services.Update](t, c); !u.ProviderID.IsEqual(remoteP) {
		t.Fatalf("got %v; want the member's provider after the backoff", u.ProviderID)
	}

	// note: the failed first attempt doubled the delay to 2*carryRetryFirst;
	// a restored member that ends again is retried after carryRetryFirst.
	node.cut()
	start := time.Now()
	expect[*services.Removed](t, c)
	if u := expect[*services.Update](t, c); !u.ProviderID.IsEqual(remoteP) {
		t.Fatalf("got %v; want the member's provider after the retry", u.ProviderID)
	}
	if elapsed := time.Since(start); elapsed >= 2*carryRetryFirst-carryRetryFirst/10 {
		t.Fatalf("retried after %v; want the reset delay %v", elapsed, carryRetryFirst)
	}
}

// emptyContracts finds no relay contract.
type emptyContracts struct{}

func (q emptyContracts) WithIssuer(*astral.Identity) authmod.ContractQueryBuilder  { return q }
func (q emptyContracts) WithSubject(*astral.Identity) authmod.ContractQueryBuilder { return q }
func (q emptyContracts) WithAction(...astral.Object) authmod.ContractQueryBuilder  { return q }
func (emptyContracts) Find(*astral.Context) ([]*auth.SignedContract, error)        { return nil, nil }

// contractlessAuth is swarmAuth with no relay contract to push.
type contractlessAuth struct {
	*swarmAuth
}

func (contractlessAuth) SignedContracts() authmod.ContractQueryBuilder { return emptyContracts{} }

// failingObjects fails every push.
type failingObjects struct {
	objects.Module
	pushes atomic.Int32
}

func (o *failingObjects) Push(*astral.Context, *astral.Identity, astral.Object) error {
	o.pushes.Add(1)
	return errors.New("push failed")
}

// The carry goes ahead when there is no relay contract to push, and when the
// push fails.
func TestSwarmCarryWithoutARelayContract(t *testing.T) {
	for name, tc := range map[string]struct {
		setup  func(*swarm) *failingObjects
		pushes int32
	}{
		"no contract": {func(s *swarm) *failingObjects {
			s.a.Auth = contractlessAuth{s.authA}
			return nil
		}, 0},
		"push fails": {func(s *swarm) *failingObjects {
			o := &failingObjects{}
			s.a.Objects = o
			return o
		}, 1},
	} {
		t.Run(name, func(t *testing.T) {
			s := newSwarm()
			failing := tc.setup(s)
			_, remoteP := advertise(t, s.b, "player", available)

			c := open(t, s.a.OpDiscover, astral.GenerateIdentity(), "services.discover?services=player&reach=swarm", false)
			if u := expect[*services.Update](t, c); !u.ProviderID.IsEqual(remoteP) {
				t.Fatalf("got %v; want the member's provider", u.ProviderID)
			}
			expect[*astral.EOS](t, c)
			if s.objectsA.pushes.Load() != 0 {
				t.Fatal("pushed a relay contract through the default objects module")
			}
			if failing != nil && failing.pushes.Load() != tc.pushes {
				t.Fatalf("made %d pushes; want %d", failing.pushes.Load(), tc.pushes)
			}
		})
	}
}

// RegisterNative refuses a set holding an invalid name and claims none of it.
func TestRegisterNativeRejectsAnInvalidName(t *testing.T) {
	mod, _ := wireModule(true)
	_, err := mod.RegisterNative([]string{"nat", ""}, &staticEvaluator{})
	if want := services.ValidateName(""); err == nil || err.Error() != want.Error() {
		t.Fatalf("got %v; want %v", err, want)
	}
	if _, err := mod.RegisterNative([]string{"nat"}, &staticEvaluator{}); err != nil {
		t.Fatalf("the refused set claimed a name: %v", err)
	}
}

// failingWriter is a caller's end that takes no bytes.
type failingWriter struct {
	once   sync.Once
	closed chan struct{}
}

func (w *failingWriter) Write([]byte) (int, error) { return 0, errors.New("caller gone") }
func (w *failingWriter) Close() error {
	w.once.Do(func() { close(w.closed) })
	return nil
}

// A failed ack ends the advertisement and releases the binding.
func TestAdvertiseReleasesTheBindingWhenTheAckFails(t *testing.T) {
	mod := serveAppsModule(&recordingAuth{verdict: true})
	caller := astral.GenerateIdentity()
	w := &failingWriter{closed: make(chan struct{})}
	_ = route(t, mod.OpAdvertise, caller, "services.advertise?services=contacts", w)

	select {
	case <-w.closed:
	case <-time.After(testWait):
		t.Fatal("the op did not close the channel")
	}
	deadline := time.Now().Add(testWait)
	for owned(mod, caller, "contacts") {
		if time.Now().After(deadline) {
			t.Fatal("a failed ack left the binding standing")
		}
		time.Sleep(time.Millisecond)
	}
}
