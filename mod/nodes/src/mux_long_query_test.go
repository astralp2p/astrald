package nodes

import (
	"bytes"
	"errors"
	"io"
	"math"
	"strings"
	"sync"
	"testing"

	"github.com/astralp2p/astral-go/astral"
	"github.com/astralp2p/astral-go/astral/channel"
)

// recordingLink is a link transport that records every byte written to it and
// never yields anything to read.
type recordingLink struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (l *recordingLink) Read([]byte) (int, error) { return 0, io.EOF }

func (l *recordingLink) Write(p []byte) (int, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.buf.Write(p)
}

func (l *recordingLink) Len() int {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.buf.Len()
}

// TestRouteQueryRejectsOverlongQueryString covers a query string too long for
// the Query frame's 16-bit length prefix: RouteQuery must reject it before
// anything reaches the link, rather than send a frame whose length wraps and
// routes a different query on the peer.
//
// why the context is already cancelled: without the guard RouteQuery would
// send the frame and then wait for a routing result that never comes; a
// cancelled context makes that path return at once, so the test fails instead
// of hanging.
func TestRouteQueryRejectsOverlongQueryString(t *testing.T) {
	link := &recordingLink{}
	localID := astral.GenerateIdentity()
	remoteID := astral.GenerateIdentity()
	m := &Mux{
		ch:             channel.New(link, channel.WithLockedWrites()),
		localIdentity:  localID,
		remoteIdentity: remoteID,
		routerSet:      make(chan struct{}),
	}

	ctx, cancel := astral.NewContext(nil).WithIdentity(localID).WithCancel()
	cancel()

	q := astral.Launch(&astral.Query{
		Nonce:       astral.NewNonce(),
		Caller:      localID,
		Target:      remoteID,
		QueryString: astral.String32(strings.Repeat("a", math.MaxUint16+5)),
	})

	_, err := m.RouteQuery(ctx, q, nil)
	var reject *astral.ErrRejected
	if !errors.As(err, &reject) || reject.Code != astral.DefaultRejectCode {
		t.Fatalf("RouteQuery err = %v; want rejection with code %v", err, astral.DefaultRejectCode)
	}

	if n := link.Len(); n != 0 {
		t.Fatalf("%d bytes written to the link; want none", n)
	}

	if _, ok := m.sessions.Get(q.Nonce); ok {
		t.Fatal("rejected query left a session registered on the mux")
	}
}
