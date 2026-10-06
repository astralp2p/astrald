package core

import (
	"context"
	"errors"
	"io"
	"sync/atomic"
	"testing"
	"time"

	"github.com/astralp2p/astral-go/astral"
	"github.com/astralp2p/astral-go/lib/routing"
)

// keepingRouter keeps the src writer it is handed and fails the route.
type keepingRouter struct {
	kept io.WriteCloser
}

func (r *keepingRouter) RouteQuery(_ *astral.Context, _ *astral.InFlightQuery, w io.WriteCloser) (io.WriteCloser, error) {
	r.kept = w
	return nil, errors.New("route failed")
}

type closeCounter struct {
	closes atomic.Int32
}

func (c *closeCounter) Write(p []byte) (int, error) { return len(p), nil }
func (c *closeCounter) Close() error                { c.closes.Add(1); return nil }

// TestRouteQueryFailureLeavesSrcClosable pins that a failed route releases the
// conn mutex, so a router that kept src can still close it, and that the close
// tolerates the never-assigned dst.
func TestRouteQueryFailureLeavesSrcClosable(t *testing.T) {
	kr := &keepingRouter{}
	r := &Router{PriorityRouter: routing.NewPriorityRouter("test")}
	r.PriorityRouter.Add(kr, 0)

	src := &closeCounter{}
	q := astral.Launch(astral.NewQuery(nil, nil, "test"))

	if _, err := r.routeQuery(astral.NewContext(context.Background()), q, src); err == nil {
		t.Fatal("routeQuery err = nil, want an error")
	}
	if kr.kept == nil {
		t.Fatal("router was not handed the src writer")
	}

	done := make(chan error, 1)
	go func() { done <- kr.kept.Close() }()

	select {
	case err := <-done:
		if err != nil {
			t.Errorf("Close err = %v, want nil", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("Close on the kept src writer blocked")
	}

	if n := src.closes.Load(); n != 1 {
		t.Errorf("src closed %d times, want 1", n)
	}
	if _, ok := r.conns.Get(q.Nonce); ok {
		t.Error("conn still registered after failed route")
	}
}
