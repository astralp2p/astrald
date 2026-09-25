package messaging

import (
	"bytes"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/astralp2p/astral-go/api/auth"
	"github.com/astralp2p/astral-go/api/messaging"
	"github.com/astralp2p/astral-go/astral"
	authmod "github.com/astralp2p/astrald/mod/auth"
)

// fakeAuth answers Authorize from a field, except whether this node hosts a
// mailbox: that question, and every other method, goes to the real auth module
// it wraps, which answers from the contracts it indexed.
//
// why hosting is never faked: it is the eligibility every other test relies
// on, so a double that answered it would test the double.
type fakeAuth struct {
	authmod.Module
	allow bool

	mu    sync.Mutex
	asked []auth.ActionObject
}

func (f *fakeAuth) Authorize(ctx *astral.Context, action auth.ActionObject) bool {
	f.record(action)

	if _, hosting := action.(*messaging.HostMailboxAction); hosting {
		return f.Module.Authorize(ctx, action)
	}
	return f.allow
}

func (f *fakeAuth) record(action auth.ActionObject) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.asked = append(f.asked, action)
}

// questions answers every action the authority was asked about, in order.
func (f *fakeAuth) questions() []auth.ActionObject {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]auth.ActionObject(nil), f.asked...)
}

type bufWriteCloser struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (b *bufWriteCloser) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.Write(p)
}

func (b *bufWriteCloser) Close() error { return nil }

func (b *bufWriteCloser) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.String()
}

// testRouterModuleWithAuth builds the router over an authority that answers
// every question but hosting as the test says.
func testRouterModuleWithAuth(t *testing.T, auth *fakeAuth) *Module {
	t.Helper()

	mod := testMessagingModule(t)
	auth.Module = authorityOf(mod)
	mod.Auth = auth
	mod.config.DeliveryTimeout = time.Second

	return mod
}

// inFlight is a query from a stranger as it arrives over a link, the way
// mod/nodes stamps one: a delivery from another node's send path.
func inFlight(target *astral.Identity, qs string) *astral.InFlightQuery {
	q := astral.Launch(astral.NewQuery(astral.GenerateIdentity(), target, qs))
	q.Extra.Set("origin", astral.OriginNetwork)
	return q
}

// A participant is a mailbox and not a service: the one query it answers
// delivers a message, and every other path reads as a target that is not there.
func TestRouteQueryRefusesAnyOtherPath(t *testing.T) {
	mod := testRouterModuleWithAuth(t, &fakeAuth{allow: true})
	participant := hostedParticipant(t, mod)

	for _, path := range []string{"chat", "", messaging.MethodMessage + ".x"} {
		_, err := mod.RouteQuery(mod.ctx, inFlight(participant, path), &bufWriteCloser{})
		if !errors.Is(err, &astral.ErrRouteNotFound{}) {
			t.Fatalf("route %q: got %v, want route not found", path, err)
		}
	}
}
