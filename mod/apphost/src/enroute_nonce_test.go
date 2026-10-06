package apphost

import (
	"io"
	"net"
	"testing"
	"time"

	"github.com/astralp2p/astral-go/api/apphost"
	"github.com/astralp2p/astral-go/astral"
	"github.com/astralp2p/astral-go/astral/channel"
)

// rejectingNode is an astral.Node whose router refuses every query, the way the
// core router refuses a nonce that is already en route.
type rejectingNode struct {
	astral.Node
}

func (rejectingNode) RouteQuery(*astral.Context, *astral.InFlightQuery, io.WriteCloser) (io.WriteCloser, error) {
	return nil, &astral.ErrRejected{}
}

// TestEnRouteNonceCollisionKeepsTheFirstEntry: a guest query reusing a nonce
// already en route must not delete the first query's entry when its own routing
// returns, or the first query becomes uncancellable and loses its owner.
func TestEnRouteNonceCollisionKeepsTheFirstEntry(t *testing.T) {
	mod, _ := ownershipModule(false)
	mod.node = rejectingNode{}

	first, second := astral.GenerateIdentity(), astral.GenerateIdentity()
	nonce, _ := enRouteQuery(mod, first)

	srvCxn, cliCxn := net.Pipe()
	defer srvCxn.Close()
	defer cliCxn.Close()

	guest := NewGuest(mod, srvCxn)
	guest.guestID = second
	client := channel.New(cliCxn)

	if err := cliCxn.SetReadDeadline(time.Now().Add(10 * time.Second)); err != nil {
		t.Fatalf("set read deadline: %v", err)
	}

	done := make(chan error, 1)
	go func() {
		done <- guest.onRouteQueryMsg(astral.NewContext(nil), &apphost.RouteQueryMsg{
			Nonce:  nonce,
			Target: astral.GenerateIdentity(),
			Query:  "test.colliding",
		})
	}()

	reply, err := client.Receive()
	if err != nil {
		t.Fatalf("receive reply: %v", err)
	}
	if _, ok := reply.(*apphost.QueryRejectedMsg); !ok {
		t.Fatalf("colliding query answered %T; want *apphost.QueryRejectedMsg", reply)
	}
	if err := <-done; err != nil {
		t.Fatalf("onRouteQueryMsg: %v", err)
	}

	entry, found := mod.enRoute.Get(nonce)
	if !found {
		t.Fatal("a colliding query deleted the first query's en-route entry")
	}
	if !entry.owner.IsEqual(first) {
		t.Fatalf("en-route owner is %v; want the first owner %v", entry.owner, first)
	}
}
