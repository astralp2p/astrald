package nodes

import (
	"context"
	"errors"
	"io"
	"net"
	"slices"
	"sync"
	"testing"
	"time"

	"github.com/astralp2p/astral-go/api/crypto"
	exonetapi "github.com/astralp2p/astral-go/api/exonet"
	"github.com/astralp2p/astral-go/api/gateway"
	"github.com/astralp2p/astral-go/api/kcp"
	"github.com/astralp2p/astral-go/api/nodes"
	"github.com/astralp2p/astral-go/api/tcp"
	"github.com/astralp2p/astral-go/api/tor"
	"github.com/astralp2p/astral-go/astral"
	"github.com/astralp2p/astral-go/sig"
	"github.com/astralp2p/astrald/mod/exonet"
)

// testRowBudget stands in for a network budget, short enough to keep the tests fast.
const testRowBudget = 200 * time.Millisecond

// stubExonet dials every endpoint with dial; the other methods are unused by the basic strategy.
type stubExonet struct {
	dial func(*astral.Context, exonetapi.Endpoint) (exonet.Conn, error)
}

func (e *stubExonet) Dial(ctx *astral.Context, ep exonetapi.Endpoint) (exonet.Conn, error) {
	return e.dial(ctx, ep)
}
func (e *stubExonet) Unpack(string, []byte) (exonetapi.Endpoint, error) {
	return nil, errors.ErrUnsupported
}
func (e *stubExonet) Parse(string, string) (exonetapi.Endpoint, error) {
	return nil, errors.ErrUnsupported
}
func (e *stubExonet) SetDialer(string, exonet.Dialer)     {}
func (e *stubExonet) SetUnpacker(string, exonet.Unpacker) {}
func (e *stubExonet) SetParser(string, exonet.Parser)     {}

// stubResolver yields a fixed list of rows for every identity.
type stubResolver struct {
	rows []*nodes.EndpointWithTTL
}

func (r *stubResolver) ResolveEndpoints(*astral.Context, *astral.Identity) (<-chan *nodes.EndpointWithTTL, error) {
	return sig.ArrayToChan(r.rows), nil
}

func newBasicTestStrategy(t *testing.T, dial func(*astral.Context, exonetapi.Endpoint) (exonet.Conn, error)) *BasicLinkStrategy {
	t.Helper()
	localKey, peerKey := newNegotiationKey(t), newNegotiationKey(t)
	mod := &Module{privateKey: &crypto.PrivateKey{Key: localKey.Serialize()}, ctx: astral.NewContext(context.Background())}
	// note: the database resolver reads the node's identity to skip rows about itself.
	mod.node = &identityNode{id: astral.IdentityFromPubKey(localKey.PubKey())}
	mod.Exonet = &stubExonet{dial: dial}
	factory := &BasicLinkStrategyFactory{
		mod:        mod,
		networks:   basicNetworks,
		rowBudgets: map[string]time.Duration{"kcp": testRowBudget, "gw": testRowBudget},
	}
	return factory.Build(astral.IdentityFromPubKey(peerKey.PubKey())).(*BasicLinkStrategy)
}

func mustKCP(t *testing.T, s string) *kcp.Endpoint {
	t.Helper()
	e, err := kcp.ParseEndpoint(s)
	if err != nil {
		t.Fatalf("kcp.ParseEndpoint(%v): %v", s, err)
	}
	return e
}

func mustTCP(t *testing.T, s string) *tcp.Endpoint {
	t.Helper()
	e, err := tcp.ParseEndpoint(s)
	if err != nil {
		t.Fatalf("tcp.ParseEndpoint(%v): %v", s, err)
	}
	return e
}

func newTestIdentity(t *testing.T) *astral.Identity {
	t.Helper()
	return astral.IdentityFromPubKey(newNegotiationKey(t).PubKey())
}

// silentPeer returns the local end of a pipe whose remote end reads everything and never writes.
func silentPeer(t *testing.T) exonet.Conn {
	t.Helper()
	a, b := net.Pipe()
	t.Cleanup(func() {
		_ = a.Close()
		_ = b.Close()
	})
	go func() { _, _ = io.Copy(io.Discard, b) }()
	return &negotiationPipeConn{Conn: a, outbound: true}
}

// tryWithin runs tryEndpoint under a context with no deadline and fails the test unless it returns nil within limit.
func tryWithin(t *testing.T, s *BasicLinkStrategy, row *nodes.EndpointWithTTL, limit time.Duration) time.Duration {
	t.Helper()
	ctx := astral.NewContext(context.Background())
	done := make(chan *Link, 1)
	start := time.Now()
	go func() { done <- s.tryEndpoint(ctx, &dialRow{endpoint: row}) }()
	select {
	case link := <-done:
		if link != nil {
			t.Fatalf("tryEndpoint(%v) returned a link; want nil", row.Endpoint)
		}
		return time.Since(start)
	case <-time.After(limit):
		t.Fatalf("tryEndpoint(%v) still running after %v; want nil within the row budget", row.Endpoint, limit)
		return 0
	}
}

func TestBasicLinkStrategyDefaults(t *testing.T) {
	f := newBasicLinkStrategyFactory(nil)
	if want := []string{"tcp", "kcp", "gw"}; !slices.Equal(f.networks, want) {
		t.Fatalf("basic networks %v; want %v", f.networks, want)
	}
	want := map[string]time.Duration{"kcp": 5 * time.Second, "gw": 20 * time.Second}
	if len(f.rowBudgets) != len(want) {
		t.Fatalf("row budgets %v; want %v", f.rowBudgets, want)
	}
	for network, d := range want {
		if f.rowBudgets[network] != d {
			t.Fatalf("row budgets %v; want %v", f.rowBudgets, want)
		}
	}
}

func TestBasicTryEndpointReleasesSilentKCPRow(t *testing.T) {
	s := newBasicTestStrategy(t, func(*astral.Context, exonetapi.Endpoint) (exonet.Conn, error) {
		return silentPeer(t), nil
	})
	elapsed := tryWithin(t, s, nodes.NewEndpointWithTTL(mustKCP(t, "127.0.0.1:1792")), 10*testRowBudget)
	if elapsed < testRowBudget/2 {
		t.Fatalf("tryEndpoint returned after %v, before the %v budget could end the handshake", elapsed, testRowBudget)
	}
}

func TestBasicTryEndpointReleasesStalledGatewayDial(t *testing.T) {
	s := newBasicTestStrategy(t, func(ctx *astral.Context, _ exonetapi.Endpoint) (exonet.Conn, error) {
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(time.Minute):
			return nil, errors.New("dial outlived the test")
		}
	})
	row := nodes.NewEndpointWithTTL(gateway.NewEndpoint(newTestIdentity(t), s.target))
	tryWithin(t, s, row, 10*testRowBudget)
}

func TestBasicTryEndpointBoundsOnlyBudgetedNetworks(t *testing.T) {
	var mu sync.Mutex
	deadlines := map[string]bool{}
	s := newBasicTestStrategy(t, func(ctx *astral.Context, ep exonetapi.Endpoint) (exonet.Conn, error) {
		_, ok := ctx.Deadline()
		mu.Lock()
		defer mu.Unlock()
		deadlines[ep.Network()] = ok
		return nil, errors.New("refused")
	})
	ctx := astral.NewContext(context.Background())
	s.tryEndpoint(ctx, &dialRow{endpoint: nodes.NewEndpointWithTTL(mustTCP(t, "127.0.0.1:1791"))})
	s.tryEndpoint(ctx, &dialRow{endpoint: nodes.NewEndpointWithTTL(mustKCP(t, "127.0.0.1:1792"))})
	want := map[string]bool{"tcp": false, "kcp": true}
	for network, has := range want {
		if deadlines[network] != has {
			t.Fatalf("dial deadlines by network %v; want %v", deadlines, want)
		}
	}
}

func TestBasicSignalDialsTCPKCPAndGatewayRows(t *testing.T) {
	var mu sync.Mutex
	var dialed []string
	s := newBasicTestStrategy(t, func(_ *astral.Context, ep exonetapi.Endpoint) (exonet.Conn, error) {
		mu.Lock()
		defer mu.Unlock()
		dialed = append(dialed, ep.Network())
		return nil, errors.New("refused")
	})
	s.mod.resolvers.Add(&stubResolver{rows: []*nodes.EndpointWithTTL{
		nodes.NewEndpointWithTTL(mustTCP(t, "127.0.0.1:1791")),
		nodes.NewEndpointWithTTL(mustKCP(t, "127.0.0.1:1792")),
		nodes.NewEndpointWithTTL(gateway.NewEndpoint(newTestIdentity(t), s.target)),
		nodes.NewEndpointWithTTL(&tor.Endpoint{}),
	}})

	done, release := s.Join(astral.NewContext(context.Background()))
	defer release()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("basic round did not finish")
	}

	mu.Lock()
	defer mu.Unlock()
	slices.Sort(dialed)
	if want := []string{"gw", "kcp", "tcp"}; !slices.Equal(dialed, want) {
		t.Fatalf("basic dialed %v; want %v", dialed, want)
	}
}
