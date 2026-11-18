package nodes

import (
	"io"
	"net"
	"testing"
	"time"

	"github.com/astralp2p/astral-go/astral"
	"github.com/astralp2p/astral-go/astral/channel"
	"github.com/astralp2p/astral-go/astral/log"
	"github.com/astralp2p/astrald/mod/nodes/frames"
)

// answeringRouter records the query it routes and answers it with reply.
type answeringRouter struct {
	reply  string
	routed chan *astral.InFlightQuery
}

func (r *answeringRouter) RouteQuery(_ *astral.Context, q *astral.InFlightQuery, w io.WriteCloser) (io.WriteCloser, error) {
	r.routed <- q
	go func() {
		w.Write([]byte(r.reply))
		w.Close()
	}()
	return nopWriteCloser{}, nil
}

type nopWriteCloser struct{}

func (nopWriteCloser) Write(p []byte) (int, error) { return len(p), nil }
func (nopWriteCloser) Close() error                { return nil }

// testMux builds a mux over conn whose read loop runs until conn closes.
//
// why the mux is built by literal: the query path needs only the channel, the
// identities, the node identity and a logger, not a Link.
func testMux(t *testing.T, conn net.Conn, localID, remoteID *astral.Identity) *Mux {
	t.Helper()
	m := &Mux{
		mod:            &Module{node: &identityNode{id: localID}, log: log.New(localID)},
		ch:             channel.New(conn, channel.WithLockedWrites()),
		localIdentity:  localID,
		remoteIdentity: remoteID,
		routerSet:      make(chan struct{}),
	}
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
	return m
}

// TestRouteQueryOwnQueryToSiblingAppIsRelayed covers a query this node
// originates for an app reached through the linked node (target ≠ the link's
// remote): the peer must route it to that target with this node as caller,
// and its answer must reach the caller instead of the query hanging.
func TestRouteQueryOwnQueryToSiblingAppIsRelayed(t *testing.T) {
	a, b := net.Pipe()
	defer a.Close()
	defer b.Close()

	localID := astral.GenerateIdentity()
	siblingID := astral.GenerateIdentity()
	appID := astral.GenerateIdentity()

	local := testMux(t, a, localID, siblingID)
	sibling := testMux(t, b, siblingID, localID)
	router := &answeringRouter{reply: "answer", routed: make(chan *astral.InFlightQuery, 1)}
	sibling.SetRouter(router)

	ctx, cancel := astral.NewContext(nil).WithIdentity(localID).WithTimeout(effectTimeout)
	defer cancel()
	q := astral.Launch(&astral.Query{
		Nonce:       astral.NewNonce(),
		Caller:      localID,
		Target:      appID,
		QueryString: "app.op",
	})

	pr, pw := io.Pipe()
	if _, err := local.RouteQuery(ctx, q, pw); err != nil {
		t.Fatalf("RouteQuery: %v; want the sibling's app to accept", err)
	}

	select {
	case got := <-router.routed:
		if !got.Target.IsEqual(appID) {
			t.Errorf("sibling routed to target %v; want the app %v", got.Target, appID)
		}
		if !got.Caller.IsEqual(localID) {
			t.Errorf("sibling routed with caller %v; want the originating node %v", got.Caller, localID)
		}
		if got.QueryString != "app.op" {
			t.Errorf("sibling routed query %q; want %q", got.QueryString, "app.op")
		}
	case <-time.After(effectTimeout):
		t.Fatal("sibling never routed the query")
	}

	answer := make(chan string, 1)
	go func() {
		buf, _ := io.ReadAll(pr)
		answer <- string(buf)
	}()
	select {
	case got := <-answer:
		if got != "answer" {
			t.Fatalf("caller read %q; want %q", got, "answer")
		}
	case <-time.After(effectTimeout):
		t.Fatal("the app's answer never reached the caller")
	}
}

// TestRouteQueryOwnQueryFraming covers the frame RouteQuery sends for a query
// this node originates: a bare Query when the target is the linked node, and a
// RelayQuery carrying caller and target otherwise.
func TestRouteQueryOwnQueryFraming(t *testing.T) {
	localID := astral.GenerateIdentity()
	remoteID := astral.GenerateIdentity()
	appID := astral.GenerateIdentity()

	for _, tc := range []struct {
		name   string
		target *astral.Identity
	}{
		{"direct to the linked node", remoteID},
		{"to an app behind the linked node", appID},
	} {
		t.Run(tc.name, func(t *testing.T) {
			a, b := net.Pipe()
			defer a.Close()
			defer b.Close()

			m := &Mux{
				ch:             channel.New(a, channel.WithLockedWrites()),
				localIdentity:  localID,
				remoteIdentity: remoteID,
				routerSet:      make(chan struct{}),
			}
			peer := channel.New(b, channel.WithLockedWrites())

			// why the peer keeps draining: net.Pipe is unbuffered, and the
			// Reset sent when the cancelled session closes must not block.
			sent := make(chan astral.Object, 1)
			go func() {
				for {
					obj, err := peer.Receive()
					if err != nil {
						return
					}
					select {
					case sent <- obj:
					default:
					}
				}
			}()

			// why cancelled after the frame is seen: RouteQuery then waits for a
			// routing result this test never sends.
			ctx, cancel := astral.NewContext(nil).WithIdentity(localID).WithCancel()
			q := astral.Launch(&astral.Query{
				Nonce:       astral.NewNonce(),
				Caller:      localID,
				Target:      tc.target,
				QueryString: "op",
			})
			done := make(chan struct{})
			go func() {
				m.RouteQuery(ctx, q, nil)
				close(done)
			}()

			var obj astral.Object
			select {
			case obj = <-sent:
			case <-time.After(effectTimeout):
				t.Fatal("RouteQuery sent no frame")
			}
			cancel()
			<-done

			if tc.target.IsEqual(remoteID) {
				f, ok := obj.(*frames.Query)
				if !ok {
					t.Fatalf("sent %T; want *frames.Query", obj)
				}
				if f.Nonce != q.Nonce || f.Query.String() != "op" {
					t.Fatalf("sent Query{Nonce: %v, Query: %q}; want {%v, %q}", f.Nonce, f.Query, q.Nonce, "op")
				}
				return
			}

			f, ok := obj.(*frames.RelayQuery)
			if !ok {
				t.Fatalf("sent %T; want *frames.RelayQuery", obj)
			}
			if !f.CallerID.IsEqual(localID) || !f.TargetID.IsEqual(appID) {
				t.Fatalf("sent RelayQuery{Caller: %v, Target: %v}; want {%v, %v}", f.CallerID, f.TargetID, localID, appID)
			}
			if f.Query.Nonce != q.Nonce || f.Query.Query.String() != "op" {
				t.Fatalf("sent RelayQuery.Query{Nonce: %v, Query: %q}; want {%v, %q}", f.Query.Nonce, f.Query.Query, q.Nonce, "op")
			}
		})
	}
}
