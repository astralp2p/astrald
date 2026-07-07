package mobile

import (
	"bytes"
	"io"
	"sync"
	"testing"
	"time"

	"github.com/cryptopunkscc/astral-go/astral"
	"github.com/cryptopunkscc/astrald/core"
	"github.com/cryptopunkscc/astrald/mod/secp256k1"
)

func TestLookupQueryHandler(t *testing.T) {
	n := NewNode()

	exact := &testHandler{}
	proto := &testHandler{}
	sub := &testHandler{}

	n.AddQueryHandler("player.play", exact)
	n.AddQueryHandler("player.", proto)
	n.AddQueryHandler("player.queue.", sub)

	if n.lookupQueryHandler("player.play") != exact {
		t.Fatal("exact name must win over prefix")
	}
	if n.lookupQueryHandler("player.pause") != proto {
		t.Fatal("prefix must match unregistered names")
	}
	if n.lookupQueryHandler("player.queue.add") != sub {
		t.Fatal("longest prefix must win")
	}
	if n.lookupQueryHandler("other.op") != nil {
		t.Fatal("unrelated names must not match")
	}

	n.RemoveQueryHandler("player.")
	if n.lookupQueryHandler("player.pause") != nil {
		t.Fatal("removed prefix must not match")
	}
}

func TestInboundQueryResolvesOnce(t *testing.T) {
	q := astral.Launch(&astral.Query{QueryString: "test.op"})
	iq := newInboundQuery(q, nopWriteCloser{})

	conn := iq.Accept()
	if conn == nil {
		t.Fatal("first Accept must succeed")
	}
	if iq.Accept() != nil {
		t.Fatal("second Accept must fail")
	}
	iq.Reject() // must be a no-op, not a panic/blocked send
}

func TestHandlerRouterEndToEnd(t *testing.T) {
	id := secp256k1.Identity(secp256k1.PublicKey(secp256k1.New()))

	cnode, err := core.NewNode(id, nil)
	if err != nil {
		t.Fatal(err)
	}

	n := NewNode()
	handler := &testHandler{
		serve: func(iq *InboundQuery) {
			if iq.Query() != "player.play?index=2" {
				t.Errorf("query = %q", iq.Query())
			}
			conn := iq.Accept()
			if conn == nil {
				t.Error("accept failed")
				return
			}
			conn.Write([]byte("response"))
			conn.Close()
		},
	}
	n.AddQueryHandler("player.", handler)

	router := &handlerRouter{node: n, cnode: cnode}

	var out bytes.Buffer
	q := astral.Launch(&astral.Query{
		Caller:      id,
		Target:      id,
		QueryString: "player.play?index=2",
	})

	ctx := astral.NewContext(nil)
	w, err := router.RouteQuery(ctx, q, writeCloser{&out})
	if err != nil {
		t.Fatalf("RouteQuery: %v", err)
	}
	w.Close()

	deadline := time.Now().Add(time.Second)
	for out.String() != "response" && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	if out.String() != "response" {
		t.Fatalf("caller received %q, want %q", out.String(), "response")
	}
}

func TestHandlerRouterRejects(t *testing.T) {
	id := secp256k1.Identity(secp256k1.PublicKey(secp256k1.New()))

	cnode, err := core.NewNode(id, nil)
	if err != nil {
		t.Fatal(err)
	}

	n := NewNode()
	n.AddQueryHandler("player.", &testHandler{
		serve: func(iq *InboundQuery) { iq.Reject() },
	})

	router := &handlerRouter{node: n, cnode: cnode}

	q := astral.Launch(&astral.Query{
		Caller:      id,
		Target:      id,
		QueryString: "player.play",
	})
	_, err = router.RouteQuery(astral.NewContext(nil), q, nopWriteCloser{})
	if err == nil {
		t.Fatal("rejected query must return an error")
	}

	// unregistered op falls through with route-not-found
	q = astral.Launch(&astral.Query{
		Caller:      id,
		Target:      id,
		QueryString: "other.op",
	})
	_, err = router.RouteQuery(astral.NewContext(nil), q, nopWriteCloser{})
	if err == nil {
		t.Fatal("unregistered op must return an error")
	}
}

type testHandler struct {
	mu    sync.Mutex
	serve func(*InboundQuery)
}

func (h *testHandler) HandleQuery(q *InboundQuery) {
	h.mu.Lock()
	serve := h.serve
	h.mu.Unlock()
	if serve != nil {
		serve(q)
	} else {
		q.Reject()
	}
}

type nopWriteCloser struct{}

func (nopWriteCloser) Write(p []byte) (int, error) { return len(p), nil }
func (nopWriteCloser) Close() error                { return nil }

type writeCloser struct{ io.Writer }

func (writeCloser) Close() error { return nil }
