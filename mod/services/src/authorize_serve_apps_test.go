package services

import (
	"errors"
	"io"
	"testing"
	"time"

	"github.com/astralp2p/astral-go/api/auth"
	"github.com/astralp2p/astral-go/astral"
	"github.com/astralp2p/astral-go/lib/query"
	"github.com/astralp2p/astral-go/lib/routing"
)

// serveAppsNode is an astral.Node that only answers Identity.
type serveAppsNode struct {
	astral.Node
	id *astral.Identity
}

func (n *serveAppsNode) Identity() *astral.Identity { return n.id }

// serveAppsModule holds the auth dependency and the two fields the advertise op
// reaches past its guard: the node's identity and the advertisement set. Every
// other field stays the zero value.
func serveAppsModule(authority *recordingAuth) *Module {
	return &Module{
		Deps:     Deps{Auth: authority},
		node:     &serveAppsNode{id: astral.GenerateIdentity()},
		external: newExternalServices(),
	}
}

// serveAppsOp is one row of the ServeApps surface in mod/services: the op, a
// query that binds its arguments, and a count of what it published.
type serveAppsOp struct {
	name      string
	op        func(*Module) any
	args      string
	installed func(*Module) int
}

func serveAppsOps() []serveAppsOp {
	return []serveAppsOp{
		{
			name:      "services.advertise",
			op:        func(m *Module) any { return m.OpAdvertise },
			args:      "?name=contacts",
			installed: func(m *Module) int { return len(m.external.snapshot()) },
		},
	}
}

// routeServeAppsFromNetwork is route for a query that arrived over a link.
func routeServeAppsFromNetwork(t *testing.T, fn any, caller *astral.Identity, queryString string, w io.WriteCloser) error {
	t.Helper()

	op, err := routing.NewOp(fn)
	if err != nil {
		t.Fatalf("new op: %v", err)
	}

	ctx, cancel := astral.NewContext(nil).WithTimeout(10 * time.Second)
	defer cancel()

	q := astral.Launch(query.New(caller, caller, queryString, nil))
	q.Extra.Set("origin", astral.OriginNetwork)

	_, err = op.RouteQuery(ctx, q, w)
	return err
}

// TestServeAppsRefusesCallerWithoutPermits is the coverage measure for the
// ServeApps action in mod/services: every hosting op must ask before it acts,
// and must reject when the answer is no.
func TestServeAppsRefusesCallerWithoutPermits(t *testing.T) {
	caller := astral.GenerateIdentity()

	for _, op := range serveAppsOps() {
		t.Run(op.name, func(t *testing.T) {
			authority := &recordingAuth{verdict: false}
			mod := serveAppsModule(authority)
			w := newRecordingWriter()

			err := route(t, op.op(mod), caller, op.name+op.args, w)

			var rejected *astral.ErrRejected
			if !errors.As(err, &rejected) {
				t.Fatalf("%s answered a caller holding no permits: got err %v, want a rejection", op.name, err)
			}

			if n := w.written(); n != 0 {
				t.Fatalf("%s wrote %d bytes to a refused caller; want none", op.name, n)
			}

			actions := authority.recorded()
			if len(actions) != 1 {
				t.Fatalf("%s made %d authorization calls; want exactly 1", op.name, len(actions))
			}

			action, ok := actions[0].(*auth.ServeAppsAction)
			if !ok {
				t.Fatalf("%s named %q; want %q", op.name, actions[0].ObjectType(), auth.ServeAppsAction{}.ObjectType())
			}

			if !action.Actor().IsEqual(caller) {
				t.Fatalf("%s named actor %v; want the caller %v", op.name, action.Actor(), caller)
			}

			if n := op.installed(mod); n != 0 {
				t.Fatalf("%s published %d advertisements for a refused caller; want none", op.name, n)
			}
		})
	}
}

// TestServeAppsKeepsTheNetworkOriginRefusal holds the refusal that predates
// ServeApps: a network caller is rejected before any authorization is asked,
// so no grant or contract lets a remote identity advertise on this node.
func TestServeAppsKeepsTheNetworkOriginRefusal(t *testing.T) {
	caller := astral.GenerateIdentity()

	for _, op := range serveAppsOps() {
		t.Run(op.name, func(t *testing.T) {
			authority := &recordingAuth{verdict: true}
			mod := serveAppsModule(authority)
			w := newRecordingWriter()

			err := routeServeAppsFromNetwork(t, op.op(mod), caller, op.name+op.args, w)

			var rejected *astral.ErrRejected
			if !errors.As(err, &rejected) {
				t.Fatalf("%s answered a network caller: got err %v, want a rejection", op.name, err)
			}

			if n := w.written(); n != 0 {
				t.Fatalf("%s wrote %d bytes to a network caller; want none", op.name, n)
			}

			if n := len(authority.recorded()); n != 0 {
				t.Fatalf("%s made %d authorization calls for a network caller; want none", op.name, n)
			}

			if n := op.installed(mod); n != 0 {
				t.Fatalf("%s published %d advertisements for a network caller; want none", op.name, n)
			}
		})
	}
}

// TestServeAppsAdvertisementStandsUntilTheSessionCloses covers the granted
// path: the advertisement names the caller as its provider, and closing the
// caller's end of the channel withdraws it.
func TestServeAppsAdvertisementStandsUntilTheSessionCloses(t *testing.T) {
	authority := &recordingAuth{verdict: true}
	mod := serveAppsModule(authority)
	caller := astral.GenerateIdentity()
	w := newRecordingWriter()

	op, err := routing.NewOp(mod.OpAdvertise)
	if err != nil {
		t.Fatalf("new op: %v", err)
	}

	ctx, cancel := astral.NewContext(nil).WithTimeout(10 * time.Second)
	defer cancel()

	conn, err := op.RouteQuery(ctx, astral.Launch(query.New(caller, caller, "services.advertise?name=contacts", nil)), w)
	if err != nil {
		t.Fatalf("services.advertise refused an authorized caller: %v", err)
	}

	for w.written() == 0 {
		if ctx.Err() != nil {
			t.Fatal("services.advertise sent no ack to an authorized caller")
		}
		time.Sleep(time.Millisecond)
	}

	ads := mod.external.snapshot()
	if len(ads) != 1 || !ads[0].ProviderID.IsEqual(caller) {
		t.Fatalf("services.advertise published %+v; want one advertisement provided by the caller", ads)
	}

	if err := conn.Close(); err != nil {
		t.Fatalf("close the caller's end: %v", err)
	}

	select {
	case <-w.closed:
	case <-ctx.Done():
		t.Fatal("services.advertise did not end when the caller closed its end")
	}

	if n := len(mod.external.snapshot()); n != 0 {
		t.Fatalf("closing the session left %d advertisements standing; want none", n)
	}
}
