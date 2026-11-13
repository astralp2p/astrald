package apphost

import (
	"errors"
	"fmt"
	"io"
	"runtime"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/astralp2p/astral-go/astral"
	"github.com/astralp2p/astral-go/lib/apps"
	"github.com/astralp2p/astral-go/lib/query"
)

// ipcTestListeners numbers the in-memory endpoints, which share one namespace in a process.
var ipcTestListeners atomic.Uint64

// ipcTestApp starts an IPC endpoint as an app does and returns the handler that routes to it and the queries it
// reads.
func ipcTestApp(t *testing.T) (*IPCHandler, <-chan *apps.PendingQuery) {
	t.Helper()

	h, err := apps.NewHandlerOn(fmt.Sprintf("memu:ipc-handler-test-%d", ipcTestListeners.Add(1)), astral.NewNonce())
	if err != nil {
		t.Fatalf("new handler: %v", err)
	}
	t.Cleanup(func() { h.Close() })

	queries := make(chan *apps.PendingQuery, 8)
	go func() {
		for {
			p, err := h.ReadQuery()
			if err != nil {
				return
			}
			queries <- p
		}
	}()

	app := astral.GenerateIdentity()
	handler := &IPCHandler{Identity: app, Owner: app, IPCToken: h.Token(), Endpoint: h.Endpoint()}

	return handler, queries
}

func ipcTestQuery(target *astral.Identity) *astral.InFlightQuery {
	return astral.Launch(query.New(astral.GenerateIdentity(), target, "test.op", nil))
}

// routingGoroutines counts the goroutines inside IPCHandler routing.
func routingGoroutines() int {
	buf := make([]byte, 1<<22)
	stacks := string(buf[:runtime.Stack(buf, true)])

	return strings.Count(stacks, "IPCHandler).RouteQuery") + strings.Count(stacks, "apphost.handshake")
}

// TestIPCHandlerRouteQueryEndsWithTheContext: an app that reads the query and never acks or rejects holds routing
// no longer than the routing context, and no routing goroutine remains.
func TestIPCHandlerRouteQueryEndsWithTheContext(t *testing.T) {
	handler, queries := ipcTestApp(t)

	ctx, cancel := astral.NewContext(nil).WithTimeout(200 * time.Millisecond)
	defer cancel()

	start := time.Now()
	done := make(chan error, 1)
	go func() {
		_, err := handler.RouteQuery(ctx, ipcTestQuery(handler.Identity), &nopWriteCloser{})
		done <- err
	}()

	select {
	case p := <-queries:
		defer p.Close()
	case <-time.After(5 * time.Second):
		t.Fatal("the app read no query")
	}

	select {
	case err := <-done:
		if err == nil {
			t.Fatal("a query the app never acked was routed")
		}
		if errors.Is(err, errEndpointUnavailable) {
			t.Fatalf("a silent app reads as an unavailable endpoint: %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatalf("routing still blocked %v after the context ended", time.Since(start))
	}
	if took := time.Since(start); took > 2*time.Second {
		t.Fatalf("routing took %v", took)
	}

	deadline := time.Now().Add(time.Second)
	for routingGoroutines() != 0 {
		if time.Now().After(deadline) {
			t.Fatalf("%d routing goroutines remain", routingGoroutines())
		}
		time.Sleep(10 * time.Millisecond)
	}
}

// TestIPCHandlerAcceptedQueryOutlivesTheContext: a query the app acks stays open in both directions after the
// routing context ends.
func TestIPCHandlerAcceptedQueryOutlivesTheContext(t *testing.T) {
	handler, queries := ipcTestApp(t)

	ctx, cancel := astral.NewContext(nil).WithTimeout(5 * time.Second)

	accepted := make(chan io.ReadWriteCloser, 1)
	go func() {
		p := <-queries
		accepted <- p.Accept()
	}()

	caller, callerEnd := io.Pipe()
	w, err := handler.RouteQuery(ctx, ipcTestQuery(handler.Identity), callerEnd)
	if err != nil {
		t.Fatalf("route: %v", err)
	}
	defer w.Close()
	cancel()

	app := <-accepted
	defer app.Close()

	// note: a memu connection is unbuffered, so a write waits for the read.
	written := make(chan error, 1)
	go func() {
		_, err := w.Write([]byte("ping"))
		written <- err
	}()
	buf := make([]byte, 4)
	if _, err = io.ReadFull(app, buf); err != nil || string(buf) != "ping" {
		t.Fatalf("the app read %q, %v", buf, err)
	}
	if err = <-written; err != nil {
		t.Fatalf("write to the app after the context ended: %v", err)
	}

	go app.Write([]byte("pong"))
	if _, err = io.ReadFull(caller, buf); err != nil || string(buf) != "pong" {
		t.Fatalf("the caller read %q, %v", buf, err)
	}
}

// TestIPCHandlerEndedContextKeepsTheEndpoint: a dial cut short by an ended context does not read as an
// unavailable endpoint.
func TestIPCHandlerEndedContextKeepsTheEndpoint(t *testing.T) {
	handler, _ := ipcTestApp(t)

	ctx, cancel := astral.NewContext(nil).WithCancel()
	cancel()

	_, err := handler.RouteQuery(ctx, ipcTestQuery(handler.Identity), &nopWriteCloser{})
	if err == nil || errors.Is(err, errEndpointUnavailable) {
		t.Fatalf("route with an ended context: %v; want route not found", err)
	}
}
