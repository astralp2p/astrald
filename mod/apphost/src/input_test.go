package apphost

import (
	"io"
	"testing"
	"time"

	"github.com/astralp2p/astral-go/api/apphost"
	"github.com/astralp2p/astral-go/api/auth"
	"github.com/astralp2p/astral-go/api/services"
	"github.com/astralp2p/astral-go/astral"
	"github.com/astralp2p/astral-go/astral/channel"
	"github.com/astralp2p/astral-go/lib/query"
	"github.com/astralp2p/astral-go/lib/routing"
)

// permits returns one unconstrained permit per action.
func permits(actions ...string) (list []*auth.Permit) {
	for _, action := range actions {
		list = append(list, &auth.Permit{Action: astral.String8(action)})
	}
	return
}

// discoveryPermit returns a discovery permit whose one scope names the services.
func discoveryPermit(names ...astral.String8) *auth.Permit {
	cs := astral.NewBundle()
	scope := &services.DiscoveryScope{Rules: []*services.DiscoveryRule{{Services: names}}}
	if err := cs.Append(scope); err != nil {
		panic(err)
	}
	return &auth.Permit{Action: astral.String8(services.ServiceDiscoveryAction{}.ObjectType()), Constraints: cs}
}

// routeSending dispatches one query from caller to one op, sends obj as the
// op's input, and returns the router's verdict. A nil obj sends nothing.
func routeSending(t *testing.T, fn any, caller *astral.Identity, queryString string, obj astral.Object, w io.WriteCloser) error {
	t.Helper()
	return routeQuerySending(t, fn, astral.Launch(query.New(caller, caller, queryString, nil)), obj, w)
}

// routeQuerySending dispatches one already-built query to one op, sends obj as
// the op's input, and returns the router's verdict. A nil obj sends nothing.
// note: the query's context lives until the test ends, so the op can read its
// input and answer after routing returns.
func routeQuerySending(t *testing.T, fn any, q *astral.InFlightQuery, obj astral.Object, w io.WriteCloser) error {
	t.Helper()

	op, err := routing.NewOp(fn)
	if err != nil {
		t.Fatalf("new op: %v", err)
	}

	ctx, cancel := astral.NewContext(nil).WithTimeout(10 * time.Second)
	t.Cleanup(cancel)

	target, err := op.RouteQuery(ctx, q, w)
	if err != nil {
		return err
	}
	// note: an op that refuses before reading closes its input, so a failed
	// send is not an error; the answer tells what happened.
	if obj != nil {
		_ = channel.NewSender(target).Send(obj)
	}
	return nil
}

// register routes apphost.register from a fresh caller with req as its input.
func register(t *testing.T, mod *Module, req *apphost.RegisterRequest, w io.WriteCloser) error {
	t.Helper()
	return routeSending(t, mod.OpRegister, astral.GenerateIdentity(), "apphost.register", req, w)
}
