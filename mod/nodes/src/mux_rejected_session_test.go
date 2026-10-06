package nodes

import (
	"errors"
	"net"
	"testing"
	"time"

	"github.com/astralp2p/astral-go/astral"
	"github.com/astralp2p/astral-go/astral/channel"
	"github.com/astralp2p/astrald/mod/nodes/frames"
)

// TestRouteQueryRejectedWithoutResetDropsSession covers a peer that refuses an
// outbound query with a Response and never follows it with a Reset, as
// handleRelayQuery's refusal does: RouteQuery must unregister the session
// itself, and must do so without sending a Reset of its own.
//
// why the mux is built by literal: RouteQuery and handleResponse need only the
// channel and the remote identity, not a *Module or a Link.
func TestRouteQueryRejectedWithoutResetDropsSession(t *testing.T) {
	local, remote := net.Pipe()
	defer local.Close()
	defer remote.Close()

	localID := astral.GenerateIdentity()
	remoteID := astral.GenerateIdentity()
	m := &Mux{
		ch:             channel.New(local, channel.WithLockedWrites()),
		localIdentity:  localID,
		remoteIdentity: remoteID,
		routerSet:      make(chan struct{}),
	}
	peer := channel.New(remote, channel.WithLockedWrites())

	// the local read loop, as Link.readLoop runs it
	go func() {
		for {
			obj, err := m.ch.Receive()
			if err != nil {
				return
			}
			if m.Handle(obj) != nil {
				return
			}
		}
	}()

	// the peer refuses the query, then reports every later frame it receives
	// (draining, so a stray Reset cannot block the marker send below)
	next := make(chan astral.Object, 8)
	go func() {
		obj, err := peer.Receive()
		if err != nil {
			return
		}
		qf, ok := obj.(*frames.Query)
		if !ok {
			return
		}
		if peer.Send(&frames.Response{Nonce: qf.Nonce, ErrCode: frames.CodeRejected}) != nil {
			return
		}
		for {
			obj, err := peer.Receive()
			if err != nil {
				return
			}
			select {
			case next <- obj:
			default:
			}
		}
	}()

	ctx := astral.NewContext(nil).WithIdentity(localID)
	q := astral.Launch(&astral.Query{
		Nonce:       astral.NewNonce(),
		Caller:      localID,
		Target:      remoteID,
		QueryString: "test",
	})

	_, err := m.RouteQuery(ctx, q, nil)
	var reject *astral.ErrRejected
	if !errors.As(err, &reject) || reject.Code != frames.CodeRejected {
		t.Fatalf("RouteQuery err = %v; want rejection with code %v", err, frames.CodeRejected)
	}

	if _, ok := m.sessions.Get(q.Nonce); ok {
		t.Fatal("rejected session is still registered on the mux")
	}

	// why a marker frame: net.Pipe is unbuffered and RouteQuery closed the
	// session before returning, so a Reset from that close would reach the
	// peer before this Ping.
	if err := m.ch.Send(&frames.Ping{Nonce: astral.NewNonce()}); err != nil {
		t.Fatalf("sending marker ping: %v", err)
	}
	select {
	case obj := <-next:
		if _, ok := obj.(*frames.Ping); !ok {
			t.Fatalf("peer received %T after the refusal; want only the marker *frames.Ping", obj)
		}
	case <-time.After(effectTimeout):
		t.Fatal("peer did not receive the marker frame")
	}
}
