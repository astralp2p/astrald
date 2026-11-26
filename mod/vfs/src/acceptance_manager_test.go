package vfs_test

import (
	"bytes"
	"testing"
	"time"

	apphostapi "github.com/astralp2p/astral-go/api/apphost"
	apiauth "github.com/astralp2p/astral-go/api/auth"
	sysapi "github.com/astralp2p/astral-go/api/sys"
	vfsapi "github.com/astralp2p/astral-go/api/vfs"
	"github.com/astralp2p/astral-go/astral"
	"github.com/astralp2p/astral-go/lib/query"
	treemod "github.com/astralp2p/astrald/mod/tree"
)

// note: a manager test appoints the manager with sys.register_manager on the fresh node, as Control Plane does,
// then claims the node: the apphost gate admits the vfs operations to an app on a claimed node only.

// appointManager appoints a new manager through sys.register_manager in a session without a token, and returns
// it as an app.
func (a *acceptanceNode) appointManager() *acceptanceApp {
	a.t.Helper()

	ch := a.mustChannel(a.tokenless(), "sys.register_manager", nil)
	if err := ch.Send(&sysapi.RegisterManagerRequest{Name: "manager"}); err != nil {
		a.t.Fatalf("send the manager request: %v", err)
	}

	answer, err := ch.Receive()
	token, ok := answer.(*apphostapi.AccessToken)
	if err != nil || !ok {
		a.t.Fatalf("sys.register_manager answered %#v, %v", answer, err)
	}

	return &acceptanceApp{id: token.Identity, token: string(token.Token), client: a.withToken(string(token.Token))}
}

// expectAck fails unless a call answered ack.
func expectAck(t *testing.T, what string, answer astral.Object, err error) {
	t.Helper()

	if _, ok := answer.(*astral.Ack); err != nil || !ok {
		t.Errorf("%v answered %#v, %v; want ack", what, answer, err)
	}
}

// managerOps are the manage operations, with arguments that name vfsID, and vfs.set_evaluator.
func managerOps(vfsID *astral.Identity) map[string]query.Args {
	return map[string]query.Args{
		"vfs.volumes":       nil,
		"vfs.create":        {"vfs": astral.GenerateIdentity()},
		"vfs.set_quota":     {"vfs": vfsID, "bytes": 1, "inodes": 1},
		"vfs.set_evaluator": {"vfs": vfsID, "endpoint": ""},
	}
}

// TestAcceptanceManagerAdministersEveryVFS (criteria 1 and 4): the appointed manager, calling locally through its
// app token with no permit, runs vfs.volumes, vfs.create, vfs.set_quota and vfs.set_evaluator on another
// identity's VFS; the same operations from the network are refused.
func TestAcceptanceManagerAdministersEveryVFS(t *testing.T) {
	a := startAcceptanceNode(t)
	manager := a.appointManager()
	if _, err := a.call(manager.client, "vfs.volumes", nil); err != nil {
		t.Logf("vfs.volumes by the manager on the unclaimed node: refused by the apphost gate: %v", err)
	}
	a.claim()
	app := a.registerApp()
	created := astral.GenerateIdentity()

	answer, err := a.call(manager.client, "vfs.volumes", nil)
	if _, ok := answer.(*vfsapi.Info); err != nil || !ok {
		t.Errorf("vfs.volumes by the manager answered %#v, %v; want info", answer, err)
	}

	answer, err = a.call(manager.client, "vfs.create", query.Args{"vfs": created})
	expectAck(t, "vfs.create by the manager", answer, err)

	answer, err = a.call(manager.client, "vfs.set_quota", query.Args{"vfs": app.id, "bytes": 7000, "inodes": 80})
	expectAck(t, "vfs.set_quota by the manager", answer, err)
	expectStatfs(t, a, app.client, app.id, 7000, 80)

	endpoint := "astral://" + manager.id.String() + ":vfs.authorize"
	answer, err = a.call(manager.client, "vfs.set_evaluator", query.Args{"vfs": app.id, "endpoint": endpoint})
	expectAck(t, "vfs.set_evaluator by the manager", answer, err)
	if got := a.evaluatorOf(app); got != endpoint {
		t.Errorf("the evaluator the manager set reads %q; want %q", got, endpoint)
	}

	for method, args := range managerOps(app.id) {
		// note: a query the router refuses outright is refused too.
		ch, err := a.fromNetwork(manager.id, method, args)
		if err != nil {
			t.Logf("%v from the network as the manager: refused by the router: %v", method, err)
			continue
		}
		answer, err := ch.Receive()
		expectError(t, method+" from the network as the manager", answer, err, vfsapi.ErrNotPermitted)
	}
}

// TestAcceptanceManagerReadsButDoesNotChangeEveryVFS (criteria 2 and 3): the appointed manager, with no permit,
// runs vfs.statfs, vfs.ls, vfs.stat and vfs.read on another app's VFS and reads its file by ObjectID; it is
// refused vfs.write, vfs.mkdir, vfs.move, vfs.remove and vfs.touch there, and the VFS stays as it was.
func TestAcceptanceManagerReadsButDoesNotChangeEveryVFS(t *testing.T) {
	a := startAcceptanceNode(t)
	manager := a.appointManager()
	a.claim()
	app := a.registerAppWith(&apphostapi.RegisterRequest{Evaluator: astral.NewBool(false)})
	file := a.mustWrite(app.client, "/docs/a.txt", []byte("hello"))
	theirs := app.id

	expectStatfs(t, a, manager.client, theirs, 1<<30, 1_000_000)

	ls := a.mustChannel(manager.client, "vfs.ls", query.Args{"vfs": theirs, "path": "/docs"})
	listed, err := ls.Receive()
	if e := expectEntry(t, "vfs.ls by the manager", listed, err); e.Name != "a.txt" {
		t.Errorf("vfs.ls by the manager listed %q; want a.txt", e.Name)
	}

	stat, err := a.call(manager.client, "vfs.stat", query.Args{"vfs": theirs, "path": "/docs/a.txt"})
	if e := expectEntry(t, "vfs.stat by the manager", stat, err); e.ID != file.ID {
		t.Errorf("vfs.stat by the manager reports %v; want %v", e.ID, file.ID)
	}

	content, err := a.readRaw(manager.client, "vfs.read", query.Args{"vfs": theirs, "path": "/docs/a.txt"})
	if err != nil || string(content) != "hello" {
		t.Errorf("vfs.read by the manager = %q, %v; want hello", content, err)
	}

	content, err = a.readRaw(manager.client, "objects.read", query.Args{"id": &file.ID})
	if err != nil || string(content) != "hello" {
		t.Errorf("objects.read of the file by the manager = %q, %v; want hello", content, err)
	}

	expectManagerChangesNothing(t, a, manager, app)
}

// expectManagerChangesNothing checks that each write operation of the manager on the VFS of app is refused and
// leaves /docs/a.txt as it was.
func expectManagerChangesNothing(t *testing.T, a *acceptanceNode, manager, app *acceptanceApp) {
	t.Helper()

	theirs := app.id
	answer, err := a.write(manager.client, query.Args{"vfs": theirs, "path": "/docs/b.txt"}, []byte("x"))
	expectError(t, "vfs.write by the manager", answer, err, vfsapi.ErrNotPermitted)

	refusals := map[string]query.Args{
		"vfs.mkdir":  {"vfs": theirs, "path": "/new"},
		"vfs.move":   {"vfs": theirs, "from": "/docs/a.txt", "to": "/docs/moved.txt"},
		"vfs.remove": {"vfs": theirs, "path": "/docs/a.txt"},
		"vfs.touch":  {"vfs": theirs, "path": "/docs/a.txt"},
	}
	for method, args := range refusals {
		answer, err := a.call(manager.client, method, args)
		expectError(t, method+" by the manager", answer, err, vfsapi.ErrNotPermitted)
	}

	content, err := a.readRaw(app.client, "vfs.read", query.Args{"path": "/docs/a.txt"})
	if err != nil || !bytes.Equal(content, []byte("hello")) {
		t.Errorf("/docs/a.txt reads %q, %v after the refusals; want hello", content, err)
	}
	for _, path := range []string{"/docs/b.txt", "/new", "/docs/moved.txt"} {
		answer, err := a.call(app.client, "vfs.stat", query.Args{"path": path})
		expectError(t, "vfs.stat "+path+" after the refusals", answer, err, vfsapi.ErrNotFound)
	}
}

// TestAcceptanceManagerContractPassesNothing (criterion 5): a contract the manager signs and the node indexes, with
// a read permit for another app's VFS and a manage permit, gives its subject neither action.
func TestAcceptanceManagerContractPassesNothing(t *testing.T) {
	a := startAcceptanceNode(t)
	manager := a.appointManager()
	a.claim()
	owner := a.registerAppWith(&apphostapi.RegisterRequest{Evaluator: astral.NewBool(false)})
	subject := a.registerAppWith(&apphostapi.RegisterRequest{Evaluator: astral.NewBool(false)})
	a.mustWrite(owner.client, "/a.txt", []byte("owned"))

	constraints := astral.NewBundle()
	if err := constraints.Append(owner.id); err != nil {
		t.Fatalf("constraint: %v", err)
	}
	signed := &apiauth.SignedContract{Contract: &apiauth.Contract{
		Issuer:  manager.id,
		Subject: subject.id,
		Permits: []*apiauth.Permit{
			{Action: astral.String8((&vfsapi.ReadAction{}).ObjectType()), Constraints: constraints},
			{Action: astral.String8((&vfsapi.ManageAction{}).ObjectType())},
		},
		ExpiresAt: astral.Time(time.Now().Add(time.Hour)),
	}}
	if err := a.auth().SignContract(a.ctx, signed); err != nil {
		t.Fatalf("sign the contract: %v", err)
	}
	if err := a.auth().IndexContract(a.ctx, signed); err != nil {
		t.Fatalf("index the contract: %v", err)
	}

	answer, err := a.call(subject.client, "vfs.statfs", query.Args{"vfs": owner.id})
	expectError(t, "vfs.statfs by the manager's subject", answer, err, vfsapi.ErrNotPermitted)
	if content, err := a.readRaw(subject.client, "vfs.read", query.Args{"vfs": owner.id, "path": "/a.txt"}); err == nil {
		t.Errorf("vfs.read by the manager's subject answered %q; want a rejection", content)
	}

	for method, args := range managerOps(owner.id) {
		answer, err := a.call(subject.client, method, args)
		expectError(t, method+" by the manager's subject", answer, err, vfsapi.ErrNotPermitted)
	}
}

// TestAcceptanceFormerManagerIsRefused (criterion 7): once the record names a new manager, the former manager is
// refused the manage and the read operations, and the new manager is allowed them.
func TestAcceptanceFormerManagerIsRefused(t *testing.T) {
	a := startAcceptanceNode(t)
	former := a.appointManager()
	a.claim()
	app := a.registerAppWith(&apphostapi.RegisterRequest{Evaluator: astral.NewBool(false)})
	successor := a.registerAppWith(&apphostapi.RegisterRequest{Evaluator: astral.NewBool(false)})

	expectStatfs(t, a, former.client, app.id, 1<<30, 1_000_000)

	// note: no operation appoints a second manager, so the test writes the record sys.register_manager writes.
	tree := a.node.Modules().Find("tree").(treemod.Module)
	if err := tree.Set(a.ctx, "/mod/sys/manager", successor.id); err != nil {
		t.Fatalf("appoint the successor: %v", err)
	}

	a.eventually("the former manager is refused vfs.statfs", func() bool {
		answer, err := a.call(former.client, "vfs.statfs", query.Args{"vfs": app.id})
		e, refused := answer.(astral.Error)
		return err == nil && refused && e.Error() == vfsapi.ErrNotPermitted.Error()
	})
	for method, args := range managerOps(app.id) {
		answer, err := a.call(former.client, method, args)
		expectError(t, method+" by the former manager", answer, err, vfsapi.ErrNotPermitted)
	}

	expectStatfs(t, a, successor.client, app.id, 1<<30, 1_000_000)
	answer, err := a.call(successor.client, "vfs.volumes", nil)
	if _, ok := answer.(*vfsapi.Info); err != nil || !ok {
		t.Errorf("vfs.volumes by the new manager answered %#v, %v; want info", answer, err)
	}
}

// managerEvaluatorBound is the longest a manager's read may take when the VFS's evaluator never answers: one
// question bounded by the auth module's default of 250 milliseconds, and the routing of the operation.
const managerEvaluatorBound = 2 * time.Second

// TestAcceptanceManagerReadWaitsAtMostOneQuestion (criterion 8): the manager reads a VFS whose evaluator accepts
// the question and never answers. The read is allowed and takes at most one bounded question, never the
// evaluator's own wait.
//
// note: the vfs and sys modules register their read handlers concurrently, so the node asks the evaluator about
// the manager on some starts and not on others; either way the read is allowed within managerEvaluatorBound.
func TestAcceptanceManagerReadWaitsAtMostOneQuestion(t *testing.T) {
	a := startAcceptanceNode(t)
	manager := a.appointManager()
	a.claim()
	owner := a.registerApp()
	a.approve(owner) // why: only an approved app serves its evaluator
	a.mustWrite(owner.client, "/a.txt", []byte("evaluated"))

	e, _ := a.serveEvaluator(owner)
	e.answer.Store(int32(answerSilent))

	for i := 0; i < 3; i++ {
		start := time.Now()
		content, err := a.readRaw(manager.client, "vfs.read", query.Args{"vfs": owner.id, "path": "/a.txt"})
		took := time.Since(start)
		if err != nil || string(content) != "evaluated" {
			t.Fatalf("vfs.read by the manager = %q, %v; want evaluated", content, err)
		}
		if took > managerEvaluatorBound {
			t.Errorf("vfs.read by the manager took %v; want at most %v", took, managerEvaluatorBound)
		}
		t.Logf("vfs.read by the manager under a silent evaluator took %v", took)
	}
}
