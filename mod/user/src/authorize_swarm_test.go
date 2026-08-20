package user

import (
	"errors"
	"io"
	"sync"
	"testing"
	"time"

	"github.com/astralp2p/astral-go/api/auth"
	"github.com/astralp2p/astral-go/api/user"
	"github.com/astralp2p/astral-go/astral"
	"github.com/astralp2p/astral-go/lib/query"
	"github.com/astralp2p/astral-go/lib/routing"
	authmod "github.com/astralp2p/astrald/mod/auth"
	"github.com/astralp2p/astrald/mod/dir"
)

// recordingAuth answers every Authorize call with `verdict` and keeps the actions
// it was asked about.
//
// why: the embedded nil interface satisfies authmod.Module without implementing
// it. Any method other than Authorize panics, which is the assertion that an op's
// refusal path touches nothing else in the auth module.
type recordingAuth struct {
	authmod.Module
	verdict bool

	mu      sync.Mutex
	actions []auth.ActionObject
}

func (a *recordingAuth) Authorize(_ *astral.Context, action auth.ActionObject) bool {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.actions = append(a.actions, action)
	return a.verdict
}

func (a *recordingAuth) recorded() []auth.ActionObject {
	a.mu.Lock()
	defer a.mu.Unlock()
	return append([]auth.ActionObject(nil), a.actions...)
}

// resolvingDir resolves every name to one identity. user.adopt and user.expel
// resolve their target before authorizing, so the refusal table needs a
// directory that answers; every other method panics.
type resolvingDir struct {
	dir.Module
	id *astral.Identity
}

func (d *resolvingDir) ResolveIdentity(string) (*astral.Identity, error) { return d.id, nil }

// recordingWriter is the caller's end of the connection. Bytes reaching it mean
// the op accepted the query and answered it.
type recordingWriter struct {
	mu     sync.Mutex
	n      int
	closed chan struct{}
	once   sync.Once
}

func newRecordingWriter() *recordingWriter { return &recordingWriter{closed: make(chan struct{})} }

func (w *recordingWriter) Write(p []byte) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.n += len(p)
	return len(p), nil
}

func (w *recordingWriter) Close() error {
	w.once.Do(func() { close(w.closed) })
	return nil
}

func (w *recordingWriter) written() int {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.n
}

// route dispatches one query to one op and returns the router's verdict once the
// op has resolved it.
func route(t *testing.T, fn any, caller *astral.Identity, queryString string, w io.WriteCloser) error {
	t.Helper()

	op, err := routing.NewOp(fn)
	if err != nil {
		t.Fatalf("new op: %v", err)
	}

	ctx, cancel := astral.NewContext(nil).WithTimeout(10 * time.Second)
	defer cancel()

	_, err = op.RouteQuery(ctx, astral.Launch(query.New(caller, caller, queryString, nil)), w)
	return err
}

// testObjectID is a well-formed id; no op in the refusal table gets far enough to
// look it up.
func testObjectID() *astral.ObjectID {
	id := &astral.ObjectID{Size: 40}
	for i := range id.Hash {
		id.Hash[i] = byte(i)
	}
	return id
}

// swarmModule is claimedModule with the two dependencies the swarm ops reach
// before they authorize. Every other field stays the zero value, so an op that
// got past its authorization check panics on a nil field — "performs no work" is
// enforced by construction as well as by the byte count.
func swarmModule(t *testing.T, authority authmod.Module, userID *astral.Identity) *Module {
	t.Helper()

	mod := claimedModule(t, userID)
	mod.Deps = Deps{Auth: authority, Dir: &resolvingDir{id: userID}}

	return mod
}

// swarmOp is one row of the swarm authorization surface: the op, a query that
// binds its required arguments, the action it must name, and the nouns the call
// is expected to declare.
//
// wantSubj asserts the action names the node the call targeted. Every row that
// sets it targets the same identity: user.adopt and user.expel resolve their
// argument through resolvingDir, and user.sync_with is handed that identity
// directly, so both arrive at userID.
type swarmOp struct {
	name       string
	op         func(*Module) any
	args       string
	admin      bool
	wantSubj   bool
	wantObject *astral.ObjectID
}

func swarmOps(id *astral.ObjectID, nodeID *astral.Identity) []swarmOp {
	return []swarmOp{
		{name: "user.info", op: func(m *Module) any { return m.OpInfo }},
		{name: "user.assets", op: func(m *Module) any { return m.OpAssets }},
		{name: "user.sync_assets", op: func(m *Module) any { return m.OpSyncAssets }},
		{name: "user.list_siblings", op: func(m *Module) any { return m.OpListSiblings }},
		{name: "user.swarm_status", op: func(m *Module) any { return m.OpSwarmStatus }},
		{name: "user.list_expelled", op: func(m *Module) any { return m.OpListExpelled }},

		{name: "user.adopt", op: func(m *Module) any { return m.OpAdopt }, args: "?target=anything", admin: true, wantSubj: true},
		{name: "user.expel", op: func(m *Module) any { return m.OpExpel }, args: "?target=anything", admin: true, wantSubj: true},
		{name: "user.add_asset", op: func(m *Module) any { return m.OpAddAsset }, args: "?id=" + id.String(), admin: true, wantObject: id},
		{name: "user.remove_asset", op: func(m *Module) any { return m.OpRemoveAsset }, args: "?id=" + id.String(), admin: true, wantObject: id},
		{name: "user.sync_with", op: func(m *Module) any { return m.OpSyncWith }, args: "?node=" + nodeID.String(), admin: true, wantSubj: true},
	}
}

// TestSwarmOpsRefuseCallerWithoutPermits is the coverage measure for the two
// swarm actions: every op in the module must ask before it acts, name the right
// action, and reject when the answer is no.
func TestSwarmOpsRefuseCallerWithoutPermits(t *testing.T) {
	id := testObjectID()
	userID := astral.GenerateIdentity()
	caller := astral.GenerateIdentity()

	for _, op := range swarmOps(id, userID) {
		t.Run(op.name, func(t *testing.T) {
			authority := &recordingAuth{verdict: false}
			mod := swarmModule(t, authority, userID)
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

			if !actions[0].Actor().IsEqual(caller) {
				t.Fatalf("%s named actor %v; want the caller %v", op.name, actions[0].Actor(), caller)
			}

			if !op.admin {
				if _, ok := actions[0].(*user.SeeSwarmAction); !ok {
					t.Fatalf("%s named %q; want %q", op.name, actions[0].ObjectType(), user.SeeSwarmAction{}.ObjectType())
				}
				return
			}

			action, ok := actions[0].(*user.AdminSwarmAction)
			if !ok {
				t.Fatalf("%s named %q; want %q", op.name, actions[0].ObjectType(), user.AdminSwarmAction{}.ObjectType())
			}

			if op.wantSubj && !action.Subject.IsEqual(userID) {
				t.Fatalf("%s declared subject %v; want the resolved target %v", op.name, action.Subject, userID)
			}

			switch {
			case (action.ObjectID == nil) != (op.wantObject == nil):
				t.Fatalf("%s declared object %v; want %v", op.name, action.ObjectID, op.wantObject)
			case action.ObjectID != nil && !action.ObjectID.IsEqual(op.wantObject):
				t.Fatalf("%s declared object %v; want %v", op.name, action.ObjectID, op.wantObject)
			}
		})
	}
}

// TestAuthorizeSwarmAllowsTheUser covers the granted path: the refusal table
// above would still pass against a handler that refuses unconditionally.
func TestAuthorizeSwarmAllowsTheUser(t *testing.T) {
	userID := astral.GenerateIdentity()
	mod := claimedModule(t, userID)

	if !mod.AuthorizeSeeSwarm(nil, &user.SeeSwarmAction{Action: auth.NewAction(userID)}) {
		t.Fatal("the active contract's issuer must be allowed to see the swarm")
	}

	if !mod.AuthorizeAdminSwarm(nil, &user.AdminSwarmAction{Action: auth.NewAction(userID)}) {
		t.Fatal("the active contract's issuer must be allowed to administer the swarm")
	}
}

// TestAuthorizeAdminSwarmRefusesEveryoneElse is the property that keeps
// user.adopt and user.expel where they were: a caller carrying no identity is
// promoted to this node's identity, so admitting anything but the issuer would
// let an unauthenticated local caller adopt and expel.
func TestAuthorizeAdminSwarmRefusesEveryoneElse(t *testing.T) {
	userID := astral.GenerateIdentity()
	mod := claimedModule(t, userID)

	for _, actor := range []*astral.Identity{astral.GenerateIdentity(), {}, nil} {
		if mod.AuthorizeAdminSwarm(nil, &user.AdminSwarmAction{Action: auth.NewAction(actor)}) {
			t.Fatalf("%v is not the issuer and must not administer the swarm", actor)
		}
	}
}

// TestAuthorizeSwarmRefusesOnUnclaimedNode holds the precondition both handlers
// share: with no active contract there is no swarm, so there is nobody to allow.
func TestAuthorizeSwarmRefusesOnUnclaimedNode(t *testing.T) {
	mod := &Module{Deps: Deps{Auth: &recordingAuth{}}}
	actor := astral.GenerateIdentity()

	if mod.AuthorizeSeeSwarm(nil, &user.SeeSwarmAction{Action: auth.NewAction(actor)}) {
		t.Fatal("an unclaimed node has no swarm to see")
	}

	if mod.AuthorizeAdminSwarm(nil, &user.AdminSwarmAction{Action: auth.NewAction(actor)}) {
		t.Fatal("an unclaimed node has no swarm to administer")
	}
}
