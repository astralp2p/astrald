package tor

import (
	"context"
	"errors"
	"net"
	"sync"
	"testing"
	"time"

	"github.com/astralp2p/astral-go/api/tor"
	"github.com/astralp2p/astral-go/api/tree"
	"github.com/astralp2p/astral-go/astral"
)

// fakeConn records that Close was called.
//
// note: the embedded nil interface panics on any other method, which asserts
// that Dial and its goroutine only ever close a conn they do not hand over.
type fakeConn struct {
	net.Conn
	once   sync.Once
	closed chan struct{}
}

func newFakeConn() *fakeConn {
	return &fakeConn{closed: make(chan struct{})}
}

func (c *fakeConn) Close() error {
	c.once.Do(func() { close(c.closed) })
	return nil
}

// fakeProxy stands in for the SOCKS5 dialer; dial runs as DialContext.
type fakeProxy struct {
	dial func(ctx context.Context) (net.Conn, error)
}

func (p fakeProxy) DialContext(ctx context.Context, _, _ string) (net.Conn, error) {
	return p.dial(ctx)
}

// dialModule builds a Module with dialing enabled and the given proxy.
func dialModule(proxy fakeProxy) *Module {
	return &Module{
		config:   Config{DialTimeout: time.Minute},
		settings: Settings{Dial: &tree.Value[*astral.Bool]{}},
		proxy:    proxy,
	}
}

func testEndpoint(t *testing.T) *tor.Endpoint {
	t.Helper()

	e, err := Parse(testServiceID + ".onion:1791")
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	return e
}

// TestDialNeverReturnsNilNil pins that Dial returns exactly one of a conn or
// an error, whichever way the proxy dial races Dial and a cancelled ctx.
func TestDialNeverReturnsNilNil(t *testing.T) {
	var errDial = errors.New("dial failed")

	var connAtOnce = func(context.Context) (net.Conn, error) {
		return newFakeConn(), nil
	}
	var errorAtOnce = func(context.Context) (net.Conn, error) {
		return nil, errDial
	}
	var connAfterCancel = func(ctx context.Context) (net.Conn, error) {
		<-ctx.Done()
		return newFakeConn(), nil
	}
	var errorAfterCancel = func(ctx context.Context) (net.Conn, error) {
		<-ctx.Done()
		return nil, ctx.Err()
	}

	var tests = []struct {
		name      string
		dial      func(ctx context.Context) (net.Conn, error)
		cancelled bool
	}{
		{"conn at once", connAtOnce, false},
		{"error at once", errorAtOnce, false},
		{"cancelled, conn at once", connAtOnce, true},
		{"cancelled, error at once", errorAtOnce, true},
		{"cancelled, conn after cancel", connAfterCancel, true},
		{"cancelled, error after cancel", errorAfterCancel, true},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			mod := dialModule(fakeProxy{dial: test.dial})
			endpoint := testEndpoint(t)

			// why: the old race needs the dial goroutine to finish before Dial
			// reaches its select, so one run proves nothing
			for i := 0; i < 10000; i++ {
				ctx, cancel := astral.NewContext(nil).WithCancel()
				if test.cancelled {
					cancel()
				}

				conn, err := mod.Dial(ctx, endpoint)
				cancel()

				if conn == nil && err == nil {
					t.Fatalf("run %d: Dial returned (nil, nil)", i)
				}
				if conn != nil && err != nil {
					t.Fatalf("run %d: Dial returned a conn and error %v", i, err)
				}
				if conn != nil {
					if c, _ := conn.(*Conn); c == nil || c.Conn == nil {
						t.Fatalf("run %d: Dial returned a conn wrapping nil", i)
					}
					conn.Close()
				}
			}
		})
	}
}

// TestDialClosesLateConn pins the late-conn contract: a conn the proxy produces
// after Dial gave up is closed by the dialing goroutine.
func TestDialClosesLateConn(t *testing.T) {
	var late = newFakeConn()
	var release = make(chan struct{})

	mod := dialModule(fakeProxy{dial: func(ctx context.Context) (net.Conn, error) {
		<-release
		return late, nil
	}})

	ctx, cancel := astral.NewContext(nil).WithCancel()
	cancel()

	conn, err := mod.Dial(ctx, testEndpoint(t))
	if conn != nil || !errors.Is(err, context.Canceled) {
		t.Fatalf("Dial = (%v, %v), want (nil, context.Canceled)", conn, err)
	}

	// note: the proxy dial completes only after Dial has returned
	close(release)

	select {
	case <-late.closed:
	case <-time.After(5 * time.Second):
		t.Fatal("late conn was not closed")
	}
}
