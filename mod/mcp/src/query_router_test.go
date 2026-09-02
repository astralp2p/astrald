package mcp

import (
	"bytes"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/astralp2p/astral-go/api/auth"
	"github.com/astralp2p/astral-go/api/mcp"
	"github.com/astralp2p/astral-go/astral"
	authmod "github.com/astralp2p/astrald/mod/auth"
)

// fakeAuth answers Authorize from a field and nothing else.
//
// why the interface is embedded rather than implemented: mcp asks auth three
// questions and a test double for all ten would say the module depends on more
// than it does. A method this reaches without a stub panics, which is the report
// that the dependency grew.
type fakeAuth struct {
	authmod.Module
	allow bool
	asked []auth.ActionObject
}

func (f *fakeAuth) Authorize(_ *astral.Context, action auth.ActionObject) bool {
	f.asked = append(f.asked, action)
	return f.allow
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

func testRouterModule(t *testing.T) *Module {
	t.Helper()
	return testRouterModuleWithAuth(t, &fakeAuth{allow: true})
}

// testRouterModuleWithAuth builds the router over an authority that answers as
// the test says.
func testRouterModuleWithAuth(t *testing.T, auth *fakeAuth) *Module {
	t.Helper()
	return &Module{
		Deps: Deps{Auth: auth},
		ctx:  astral.NewContext(nil),
		config: Config{
			QueryTimeout:       time.Second,
			MaxPayloadBytes:    64 << 10,
			MaxResponseBytes:   64 << 10,
			MaxResponseObjects: 64,
		},
	}
}

func inFlight(target *astral.Identity, qs string) *astral.InFlightQuery {
	return astral.Launch(astral.NewQuery(astral.GenerateIdentity(), target, qs))
}

// An agent is a mailbox and not a service: the one query it answers delivers a
// message, and every other path reads as a target that is not there.
func TestRouteQueryRefusesAnyOtherPath(t *testing.T) {
	mod := testRouterModule(t)
	agentID := registeredAgent(mod)

	for _, path := range []string{"chat", "", mcp.MethodMessage + ".x"} {
		_, err := mod.RouteQuery(mod.ctx, inFlight(agentID, path), &bufWriteCloser{})
		if !errors.Is(err, &astral.ErrRouteNotFound{}) {
			t.Fatalf("route %q: got %v, want route not found", path, err)
		}
	}
}
