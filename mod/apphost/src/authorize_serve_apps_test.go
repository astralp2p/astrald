package apphost

import (
	"io"
	"net"
	"slices"
	"testing"
	"time"

	"github.com/astralp2p/astral-go/api/apphost"
	"github.com/astralp2p/astral-go/api/auth"
	"github.com/astralp2p/astral-go/api/crypto"
	"github.com/astralp2p/astral-go/astral"
	"github.com/astralp2p/astral-go/astral/channel"
	"github.com/astralp2p/astral-go/astral/log"
	"github.com/astralp2p/astral-go/lib/query"
	"github.com/astralp2p/astral-go/lib/routing"
	"github.com/astralp2p/astrald/lib/policyconf"
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

			if err != nil {
				t.Fatalf("route: %v", err)
			}
			if got, want := w.refusal(t), auth.ErrNotPermitted.Error(); got != want {
				t.Fatalf("answered %q; want %q", got, want)
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

			if err != nil {
				t.Fatalf("route: %v", err)
			}
			if got, want := w.refusal(t), astral.NewErrOriginNotAllowed(astral.OriginNetwork).Error(); got != want {
				t.Fatalf("answered %q; want %q", got, want)
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

// TestHostingPermitsLeadTheRequest covers what the register policy is handed:
// ServeApps and UseBlueprints lead the grant permits whether or not the app
// asked for anything, and the app's own ask follows them unchanged.
func TestHostingPermitsLeadTheRequest(t *testing.T) {
	defaults := []string{auth.ServeAppsAction{}.ObjectType(), auth.UseBlueprintsAction{}.ObjectType()}

	for _, asked := range [][]string{nil, {"mod.user.see_swarm_action"}} {
		requested := withHostingPermits(permits(asked...))
		want := append(append([]string{}, defaults...), asked...)

		if got := actions(requested); !slices.Equal(got, want) {
			t.Fatalf("asked %q: the policy is handed %v; want %v", asked, got, want)
		}

		for _, p := range requested[:len(defaults)] {
			if p.Constraints != nil {
				t.Fatalf("asked %q: the default %s permit carries constraints", asked, p.Action)
			}
		}
	}
}

// registerService sends one register_service_msg for host from a guest
// authenticated as guestID and returns the host's reply. A handler it installs
// lives until the test ends.
func registerService(t *testing.T, mod *Module, guestID, host *astral.Identity) astral.Object {
	t.Helper()

	srvConn, cliConn := net.Pipe()
	t.Cleanup(func() { srvConn.Close(); cliConn.Close() })

	guest := NewGuestFromChannel(mod, channel.New(srvConn, channel.WithLockedWrites()), srvConn, ModeBinary)
	guest.guestID = guestID

	ctx, cancel := astral.NewContext(nil).WithCancel()
	t.Cleanup(cancel)

	done := make(chan error, 1)
	go func() {
		done <- guest.onRegisterServiceMsg(ctx, &apphost.RegisterServiceMsg{Identity: host})
	}()

	reply, err := channel.New(cliConn).Receive()
	if err != nil {
		t.Fatalf("receive the reply to register_service_msg: %v", err)
	}

	if err := <-done; err != nil {
		t.Fatalf("register_service_msg: %v", err)
	}

	return reply
}

// TestServeAppsRegisterServiceRefusesAHostWithoutPermits holds the WebSocket
// hosting path to the op's bar: an authenticated guest hosting as itself is
// denied without ServeApps, and no handler is installed.
func TestServeAppsRegisterServiceRefusesAHostWithoutPermits(t *testing.T) {
	authority := &recordingAuth{verdict: false}
	mod := &Module{Deps: Deps{Auth: authority}, log: log.New(nil)}
	host := astral.GenerateIdentity()

	reply := registerService(t, mod, host, host)

	refusal, ok := reply.(*apphost.ErrorMsg)
	if !ok || refusal.Code != apphost.ErrCodeDenied {
		t.Fatalf("register_service_msg answered a host holding no permits with %v; want error_msg{%v}", reply.ObjectType(), apphost.ErrCodeDenied)
	}

	actions := authority.recorded()
	if len(actions) != 1 {
		t.Fatalf("register_service_msg made %d authorization calls; want exactly 1", len(actions))
	}

	action, ok := actions[0].(*auth.ServeAppsAction)
	if !ok {
		t.Fatalf("register_service_msg named %q; want %q", actions[0].ObjectType(), auth.ServeAppsAction{}.ObjectType())
	}

	if !action.Actor().IsEqual(host) {
		t.Fatalf("register_service_msg named actor %v; want the host %v", action.Actor(), host)
	}

	if n := len(mod.serviceHandlers.Clone()); n != 0 {
		t.Fatalf("register_service_msg installed %d handlers for a refused host; want none", n)
	}
}

// TestServeAppsRegisterServiceNamesTheHostedIdentity covers the granted sudo
// path: ServeApps is asked for the identity the handler answers for, never for
// the guest acting as it.
func TestServeAppsRegisterServiceNamesTheHostedIdentity(t *testing.T) {
	authority := &recordingAuth{verdict: true}
	mod := &Module{Deps: Deps{Auth: authority}, log: log.New(nil)}
	guestID := astral.GenerateIdentity()
	host := astral.GenerateIdentity()

	reply := registerService(t, mod, guestID, host)

	if _, ok := reply.(*astral.Ack); !ok {
		t.Fatalf("register_service_msg answered an authorized host with %v; want ack", reply.ObjectType())
	}

	actions := authority.recorded()
	if len(actions) != 2 {
		t.Fatalf("register_service_msg made %d authorization calls; want sudo then ServeApps", len(actions))
	}

	action, ok := actions[1].(*auth.ServeAppsAction)
	if !ok {
		t.Fatalf("register_service_msg named %q second; want %q", actions[1].ObjectType(), auth.ServeAppsAction{}.ObjectType())
	}

	if !action.Actor().IsEqual(host) {
		t.Fatalf("register_service_msg named actor %v; want the hosted identity %v", action.Actor(), host)
	}

	handlers := mod.serviceHandlers.Clone()
	if len(handlers) != 1 || !handlers[0].Identity.IsEqual(host) {
		t.Fatalf("register_service_msg installed %d handlers; want 1 for %v", len(handlers), host)
	}
}

// serveAppsSigner signs and indexes every contract without checking it.
type serveAppsSigner struct{ authmod.Module }

func (serveAppsSigner) SignContract(*astral.Context, *auth.SignedContract) error { return nil }
func (serveAppsSigner) SignSubject(*astral.Context, *auth.SignedContract) (*crypto.Signature, error) {
	return nil, nil
}
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

// serveAppsRegistrar is a module that completes apphost.register: tokens
// live in an in-memory database, and signing and storing succeed.
func serveAppsRegistrar(t *testing.T) *Module {
	t.Helper()

	gdb, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatalf("open db: %v", err)
	}

	// note: every connection to :memory: opens its own database, so the pool
	// holds one.
	sqlDB, err := gdb.DB()
	if err != nil {
		t.Fatalf("sql db: %v", err)
	}
	sqlDB.SetMaxOpenConns(1)

	db := &DB{DB: gdb}
	if err := db.migrate(); err != nil {
		t.Fatalf("migrate: %v", err)
	}

	// why accept_all: a registrar under the default reject_all writes nothing.
	config := defaultConfig()
	config.Policies.AppRegister = policyconf.Setting{Mode: policyconf.AcceptAll}

	return &Module{
		Deps:   Deps{Auth: serveAppsSigner{}, Crypto: serveAppsKeys{}, Objects: serveAppsStore{}, VFS: &recordingVFS{}},
		config: config,
		node:   &serveAppsNode{id: astral.GenerateIdentity()},
		log:    log.New(nil),
		db:     db,
	}
}

// permitSigner records every contract SignContract signs: the node→app
// contract of a registration's permits.
type permitSigner struct {
	serveAppsSigner
	signed chan *auth.Contract
}

func (s permitSigner) SignContract(_ *astral.Context, sc *auth.SignedContract) error {
	s.signed <- sc.Contract
	return nil
}

// TestServeAppsRegistrationSignsTheHostingPermits is the operator's decision
// end to end: under the accept_all register policy, an app that asks for
// nothing leaves apphost.register holding a node→app contract of ServeApps
// and UseBlueprints.
func TestServeAppsRegistrationSignsTheHostingPermits(t *testing.T) {
	mod := serveAppsRegistrar(t)
	signer := permitSigner{signed: make(chan *auth.Contract, 4)}
	mod.Auth = signer
	w := newRecordingWriter()

	if err := register(t, mod, &apphost.RegisterRequest{}, w); err != nil {
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
	if len(signer.signed) != 1 {
		t.Fatalf("apphost.register signed %d permit contracts; want 1", len(signer.signed))
	}

	c := <-signer.signed
	app := tokens[0].Identity
	if !c.Issuer.IsEqual(mod.node.Identity()) || !c.Subject.IsEqual(app) {
		t.Fatalf("the permit contract runs %v -> %v; want the node -> %v", c.Issuer, c.Subject, app)
	}
	want := []string{auth.ServeAppsAction{}.ObjectType(), auth.UseBlueprintsAction{}.ObjectType()}
	if got := actions(c.Permits); !slices.Equal(got, want) {
		t.Fatalf("the permit contract holds %v; want %v", got, want)
	}
}

// TestProvisionAppSignsGrantPermitsUndelegable: sys.register_manager passes the
// manager's grant permits through ProvisionApp, which signs them, as
// apphost.register does, with Delegation zero, and its contract permits with
// the Delegation they carry.
func TestProvisionAppSignsGrantPermitsUndelegable(t *testing.T) {
	mod := serveAppsRegistrar(t)
	signer := permitSigner{signed: make(chan *auth.Contract, 4)}
	mod.Auth = signer

	grant := &auth.Permit{Action: "mod.user.see_swarm_action", Delegation: 2}
	contract := &auth.Permit{Action: "mod.user.shell_action", Delegation: 2}
	if _, err := mod.ProvisionApp(astral.NewContext(nil), []*auth.Permit{grant}, []*auth.Permit{contract}); err != nil {
		t.Fatalf("ProvisionApp: %v", err)
	}
	if len(signer.signed) != 1 {
		t.Fatalf("ProvisionApp signed %d permit contracts; want 1", len(signer.signed))
	}
	c := <-signer.signed
	delegation := map[string]uint8{}
	for _, p := range c.Permits {
		delegation[string(p.Action)] = uint8(p.Delegation)
	}
	if d, ok := delegation[string(grant.Action)]; !ok || d != 0 {
		t.Fatalf("the grant permit was signed with Delegation %d (present %v); want 0", d, ok)
	}
	if d := delegation[string(contract.Action)]; d != 2 {
		t.Fatalf("the contract permit was signed with Delegation %d; want 2", d)
	}
}
