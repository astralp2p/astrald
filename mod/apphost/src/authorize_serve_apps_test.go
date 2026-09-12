package apphost

import (
	"errors"
	"io"
	"testing"
	"time"

	"github.com/astralp2p/astral-go/api/auth"
	"github.com/astralp2p/astral-go/astral"
	"github.com/astralp2p/astral-go/astral/log"
	"github.com/astralp2p/astral-go/lib/query"
	"github.com/astralp2p/astral-go/lib/routing"
	authmod "github.com/astralp2p/astrald/mod/auth"
	cryptomod "github.com/astralp2p/astrald/mod/crypto"
	objectsmod "github.com/astralp2p/astrald/mod/objects"
	"github.com/glebarez/sqlite"
	"gorm.io/gorm"
)

// serveAppsOp is one row of the ServeApps surface in mod/apphost: the op, a
// query that binds its required arguments, and a count of what it installed.
type serveAppsOp struct {
	name      string
	op        func(*Module) any
	args      string
	installed func(*Module) int
}

func serveAppsOps() []serveAppsOp {
	return []serveAppsOp{
		{
			name:      "apphost.register_handler",
			op:        func(m *Module) any { return m.OpRegisterHandler },
			args:      "?endpoint=tcp:127.0.0.1:9001&token=a3f1c2d4e5b6f708",
			installed: func(m *Module) int { return len(m.ipcHandlers.Clone()) },
		},
	}
}

func serveAppsAction(actor *astral.Identity) *auth.ServeAppsAction {
	return &auth.ServeAppsAction{Action: auth.NewAction(actor)}
}

func serveAppsPermit() *auth.Permit {
	return &auth.Permit{Action: astral.String8(auth.ServeAppsAction{}.ObjectType())}
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

// awaitServeAppsClose waits for the op to close the caller's connection, which
// it does on return.
func awaitServeAppsClose(t *testing.T, w *recordingWriter) {
	t.Helper()

	select {
	case <-w.closed:
	case <-time.After(10 * time.Second):
		t.Fatal("the op never closed the caller's connection")
	}
}

// TestServeAppsRefusesCallerWithoutPermits is the coverage measure for the
// ServeApps action in mod/apphost: every hosting op must ask before it acts,
// and must reject when the answer is no.
//
// The module is a bare struct holding only the auth dependency. An op that
// reached past its authorization check writes an ack or panics on a nil field,
// so "installs nothing" is enforced by the byte count as well as the handler
// count.
func TestServeAppsRefusesCallerWithoutPermits(t *testing.T) {
	caller := astral.GenerateIdentity()

	for _, op := range serveAppsOps() {
		t.Run(op.name, func(t *testing.T) {
			authority := &recordingAuth{verdict: false}
			mod := &Module{Deps: Deps{Auth: authority}}
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
				t.Fatalf("%s installed %d handlers for a refused caller; want none", op.name, n)
			}
		})
	}
}

// TestServeAppsKeepsTheNetworkOriginRefusal holds the refusal that predates
// ServeApps: a network caller is rejected before any authorization is asked,
// so no grant or contract lets a remote identity host on this node.
func TestServeAppsKeepsTheNetworkOriginRefusal(t *testing.T) {
	caller := astral.GenerateIdentity()

	for _, op := range serveAppsOps() {
		t.Run(op.name, func(t *testing.T) {
			authority := &recordingAuth{verdict: true}
			mod := &Module{Deps: Deps{Auth: authority}}
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
				t.Fatalf("%s installed %d handlers for a network caller; want none", op.name, n)
			}
		})
	}
}

// TestServeAppsRegisterHandlerInstallsForTheCaller covers the granted path: the
// refusal table would still pass against an op that refuses unconditionally.
// The handler answers for the caller, never for an identity the query names.
func TestServeAppsRegisterHandlerInstallsForTheCaller(t *testing.T) {
	authority := &recordingAuth{verdict: true}
	mod := &Module{Deps: Deps{Auth: authority}, log: log.New(nil)}
	caller := astral.GenerateIdentity()
	w := newRecordingWriter()
	op := serveAppsOps()[0]

	if err := route(t, op.op(mod), caller, op.name+op.args, w); err != nil {
		t.Fatalf("%s refused an authorized caller: %v", op.name, err)
	}
	awaitServeAppsClose(t, w)

	if w.written() == 0 {
		t.Fatalf("%s sent no ack to an authorized caller", op.name)
	}

	handlers := mod.ipcHandlers.Clone()
	if len(handlers) != 1 {
		t.Fatalf("%s installed %d handlers; want 1", op.name, len(handlers))
	}

	if !handlers[0].Identity.IsEqual(caller) {
		t.Fatalf("%s installed a handler for %v; want the caller %v", op.name, handlers[0].Identity, caller)
	}
}

// TestServeAppsGrantAuthorizesTheGrantee covers the apphost shim: a node-local
// ServeApps grant authorizes its grantee and nobody else.
func TestServeAppsGrantAuthorizesTheGrantee(t *testing.T) {
	mod := testGrantModule(t)
	app := astral.GenerateIdentity()

	if err := mod.Grant(app, serveAppsPermit(), nil); err != nil {
		t.Fatalf("grant: %v", err)
	}

	if !mod.AuthorizeServeApps(nil, serveAppsAction(app)) {
		t.Fatal("a ServeApps grant was refused to its grantee")
	}

	if mod.AuthorizeServeApps(nil, serveAppsAction(astral.GenerateIdentity())) {
		t.Fatal("an identity holding no ServeApps grant was allowed")
	}
}

// TestServeAppsGrantRefusesAConstrainedPermit holds the action's bar: ServeApps
// evaluates no constraint, so a narrowed grant must not be honoured in full.
func TestServeAppsGrantRefusesAConstrainedPermit(t *testing.T) {
	mod := testGrantModule(t)
	app := astral.GenerateIdentity()

	permit := serveAppsPermit()
	permit.Constraints = astral.NewBundle()
	if err := permit.Constraints.Append(astral.NewString8("contacts")); err != nil {
		t.Fatalf("constrain: %v", err)
	}

	if err := mod.Grant(app, permit, nil); err != nil {
		t.Fatalf("grant: %v", err)
	}

	if mod.AuthorizeServeApps(nil, serveAppsAction(app)) {
		t.Fatal("a constrained ServeApps grant was honoured")
	}
}

// TestServeAppsJoinsEveryGrantRequest covers what the register policy is
// handed: ServeApps leads the grant request whether or not the app asked for
// anything, and the app's own ask follows it unchanged.
func TestServeAppsJoinsEveryGrantRequest(t *testing.T) {
	serveApps := astral.String8(auth.ServeAppsAction{}.ObjectType())

	for _, asked := range []string{"", "mod.user.see_swarm_action"} {
		requested := serveAppsGrantRequest(asked)
		want := append([]string{string(serveApps)}, actions(parsePermits(asked))...)

		if got := actions(requested); len(got) != len(want) || got[0] != want[0] || got[len(got)-1] != want[len(want)-1] {
			t.Fatalf("asked %q: the policy is handed %v; want %v", asked, got, want)
		}

		if requested[0].Constraints != nil {
			t.Fatalf("asked %q: the ServeApps permit carries constraints", asked)
		}
	}
}

// serveAppsSigner signs and indexes every contract without checking it.
type serveAppsSigner struct{ authmod.Module }

func (serveAppsSigner) SignContract(*astral.Context, *auth.SignedContract) error  { return nil }
func (serveAppsSigner) IndexContract(*astral.Context, *auth.SignedContract) error { return nil }

// serveAppsKeys accepts every key into the index.
type serveAppsKeys struct{ cryptomod.Module }

func (serveAppsKeys) AddToIndex(astral.Object) error { return nil }

// serveAppsStore stores every object and keeps none of them.
type serveAppsStore struct{ objectsmod.Module }

func (serveAppsStore) WriteDefault() objectsmod.Repository { return nil }

func (serveAppsStore) Store(*astral.Context, objectsmod.Repository, astral.Object) (*astral.ObjectID, error) {
	return &astral.ObjectID{}, nil
}

// serveAppsNode is an astral.Node that only answers Identity.
type serveAppsNode struct {
	astral.Node
	id *astral.Identity
}

func (n *serveAppsNode) Identity() *astral.Identity { return n.id }

// serveAppsRegistrar is a module that completes apphost.register: grants and
// tokens live in an in-memory database, and signing and storing succeed.
func serveAppsRegistrar(t *testing.T) *Module {
	t.Helper()

	gdb, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatalf("open db: %v", err)
	}

	db := &DB{DB: gdb}
	if err := db.AutoMigrate(&dbGrant{}, &dbAccessToken{}); err != nil {
		t.Fatalf("migrate: %v", err)
	}

	return &Module{
		Deps:   Deps{Auth: serveAppsSigner{}, Crypto: serveAppsKeys{}, Objects: serveAppsStore{}},
		config: defaultConfig,
		node:   &serveAppsNode{id: astral.GenerateIdentity()},
		log:    log.New(nil),
		db:     db,
	}
}

// TestServeAppsRegistrationWritesTheGrant is the operator's decision end to end:
// under the default register policy, an app that asks for nothing leaves
// apphost.register holding a ServeApps grant.
func TestServeAppsRegistrationWritesTheGrant(t *testing.T) {
	mod := serveAppsRegistrar(t)
	w := newRecordingWriter()

	if err := route(t, mod.OpRegister, astral.GenerateIdentity(), "apphost.register", w); err != nil {
		t.Fatalf("apphost.register refused: %v", err)
	}
	awaitServeAppsClose(t, w)

	var tokens []dbAccessToken
	if err := mod.db.Find(&tokens).Error; err != nil {
		t.Fatalf("list tokens: %v", err)
	}
	if len(tokens) != 1 {
		t.Fatalf("apphost.register issued %d tokens; want 1", len(tokens))
	}

	app := tokens[0].Identity
	if !mod.AuthorizeServeApps(nil, serveAppsAction(app)) {
		t.Fatalf("the app registered as %v holds no ServeApps grant", app)
	}
}
