package vfs

import (
	"errors"
	"strings"
	"testing"
	"time"

	apiauth "github.com/astralp2p/astral-go/api/auth"
	vfsapi "github.com/astralp2p/astral-go/api/vfs"
	"github.com/astralp2p/astral-go/astral"
	authmod "github.com/astralp2p/astrald/mod/auth"
)

// fakeEvaluator answers every question the same way and records each question it is put.
type fakeEvaluator struct {
	allow bool
	err   error
	asked []apiauth.ActionObject
	// actors holds the actor of each question as it was asked; the auth module restores it after a chain step.
	actors []*astral.Identity
}

func (e *fakeEvaluator) Ask(_ *astral.Context, action apiauth.ActionObject) (bool, error) {
	e.asked = append(e.asked, action)
	e.actors = append(e.actors, action.Actor())

	return e.allow, e.err
}

// errNotRunning is the answer of an evaluator nobody serves.
var errNotRunning = errors.New("route not found")

// evaluatorAuth is the real auth module with the asker of each endpoint replaced by a fake evaluator. The
// endpoint is checked by the real module first. An endpoint without a fake is an evaluator that is not running.
type evaluatorAuth struct {
	authmod.Module
	evaluators map[string]*fakeEvaluator
}

func (a *evaluatorAuth) NewAuthorizeAsk(endpoint string, timeout time.Duration) (apiauth.AuthorizeAsk, error) {
	if _, err := a.Module.NewAuthorizeAsk(endpoint, timeout); err != nil {
		return nil, err
	}

	e, ok := a.evaluators[endpoint]
	if !ok {
		e = &fakeEvaluator{err: errNotRunning}
		a.evaluators[endpoint] = e
	}

	return e, nil
}

// questions returns the number of questions put to every evaluator.
func (a *evaluatorAuth) questions() (n int) {
	for _, e := range a.evaluators {
		n += len(e.asked)
	}

	return n
}

// evaluatorFixture is the vfs module over the real auth module, on a node with a user, with fake evaluators.
type evaluatorFixture struct {
	mod  *Module
	auth *evaluatorAuth
}

func newEvaluatorFixture(t *testing.T) *evaluatorFixture {
	t.Helper()

	mod, authority := authzModule(t)
	f := &evaluatorFixture{mod: mod, auth: &evaluatorAuth{Module: authority, evaluators: map[string]*fakeEvaluator{}}}
	mod.Auth = f.auth

	return f
}

// vfsWithEvaluator creates a VFS whose evaluator is a new fake that answers allow.
func (f *evaluatorFixture) vfsWithEvaluator(t *testing.T, allow bool) (*astral.Identity, *fakeEvaluator) {
	t.Helper()

	vfs := astral.GenerateIdentity()
	createVFS(t, f.mod, vfs)

	endpoint := "astral://" + astral.GenerateIdentity().String() + ":" + vfsapi.MethodAuthorize
	e := &fakeEvaluator{allow: allow}
	f.auth.evaluators[endpoint] = e

	if err := f.mod.SetEvaluator(vfs, endpoint); err != nil {
		t.Fatalf("SetEvaluator: %v", err)
	}

	return vfs, e
}

// access reports whether the caller may read and whether it may write the VFS, as an operation asks.
func (f *evaluatorFixture) access(caller, vfs *astral.Identity) (read, write bool) {
	ctx := testContext()
	_, rerr := f.mod.authorizeVFS(ctx, caller, vfs, accessRead)
	_, werr := f.mod.authorizeVFS(ctx, caller, vfs, accessWrite)

	return rerr == nil, werr == nil
}

func TestAnEmptyEvaluatorAsksNobody(t *testing.T) {
	f := newEvaluatorFixture(t)
	vfs := astral.GenerateIdentity()
	createVFS(t, f.mod, vfs)

	if read, write := f.access(astral.GenerateIdentity(), vfs); read || write {
		t.Fatalf("a caller without a permit reads %v, writes %v", read, write)
	}
	if n := f.auth.questions(); n != 0 {
		t.Fatalf("%d questions were put about a VFS without an evaluator", n)
	}
}

func TestAnEvaluatorThatAnswersYesAllowsReadAndWrite(t *testing.T) {
	f := newEvaluatorFixture(t)
	vfs, e := f.vfsWithEvaluator(t, true)
	caller := astral.GenerateIdentity()

	if read, write := f.access(caller, vfs); !read || !write {
		t.Fatalf("a caller the evaluator allows reads %v, writes %v", read, write)
	}

	if len(e.asked) != 2 {
		t.Fatalf("the evaluator was asked %d times, want 2", len(e.asked))
	}
	if _, ok := e.asked[0].(*vfsapi.ReadAction); !ok {
		t.Errorf("the first question is %T, want a read action", e.asked[0])
	}
	if _, ok := e.asked[1].(*vfsapi.WriteAction); !ok {
		t.Errorf("the second question is %T, want a write action", e.asked[1])
	}
	for i, actor := range e.actors {
		if !actor.IsEqual(caller) {
			t.Errorf("question %d names actor %v, want the caller", i, actor)
		}
	}
}

func TestAnEvaluatorThatAnswersNoRefuses(t *testing.T) {
	f := newEvaluatorFixture(t)
	vfs, e := f.vfsWithEvaluator(t, false)

	if read, write := f.access(astral.GenerateIdentity(), vfs); read || write {
		t.Fatalf("a caller the evaluator refuses reads %v, writes %v", read, write)
	}
	if len(e.asked) != 2 {
		t.Fatalf("the evaluator was asked %d times, want 2", len(e.asked))
	}
}

// TestAnEvaluatorThatFailsRefuses: an error stands for an evaluator that does not answer in time or answers with
// an error. An evaluator nobody serves refuses too.
func TestAnEvaluatorThatFailsRefuses(t *testing.T) {
	f := newEvaluatorFixture(t)
	vfs, e := f.vfsWithEvaluator(t, true)
	e.err = errors.New("no answer")

	if read, write := f.access(astral.GenerateIdentity(), vfs); read || write {
		t.Fatalf("a failing evaluator lets a caller read %v, write %v", read, write)
	}

	unasked := astral.GenerateIdentity()
	createVFS(t, f.mod, unasked)
	if err := f.mod.SetEvaluator(unasked, "astral://nobody:vfs.authorize"); err != nil {
		t.Fatal(err)
	}
	if read, write := f.access(astral.GenerateIdentity(), unasked); read || write {
		t.Fatalf("an evaluator that cannot be asked lets a caller read %v, write %v", read, write)
	}
}

func TestTheVFSIdentityAndRootCallersAreNotAsked(t *testing.T) {
	f := newEvaluatorFixture(t)
	vfs, e := f.vfsWithEvaluator(t, false)

	for name, caller := range map[string]*astral.Identity{"vfs": vfs, "user": f.mod.User.Identity(), "node": f.mod.identity} {
		if read, write := f.access(caller, vfs); !read || !write {
			t.Errorf("the %v reads %v, writes %v", name, read, write)
		}
		if f.auth.Authorize(testContext(), readAction(caller, vfs)) != true {
			t.Errorf("the auth module refuses the %v", name)
		}
	}

	if len(e.asked) != 0 {
		t.Fatalf("the evaluator was asked %d times about callers the rules allow", len(e.asked))
	}
}

func TestAnEvaluatorIsAskedAboutItsOwnVFSOnly(t *testing.T) {
	f := newEvaluatorFixture(t)
	vfsA, evalA := f.vfsWithEvaluator(t, true)
	vfsB, evalB := f.vfsWithEvaluator(t, false)
	plain := astral.GenerateIdentity()
	createVFS(t, f.mod, plain)
	caller := astral.GenerateIdentity()

	f.access(caller, vfsB)
	f.access(caller, plain)
	f.access(caller, astral.GenerateIdentity())

	if len(evalA.asked) != 0 {
		t.Fatalf("the evaluator of %v was asked %d times about other VFSes", vfsA, len(evalA.asked))
	}

	f.access(caller, vfsA)
	for name, e := range map[string]*fakeEvaluator{"A": evalA, "B": evalB} {
		want := map[string]*astral.Identity{"A": vfsA, "B": vfsB}[name]
		for _, q := range e.asked {
			var got *astral.Identity
			switch a := q.(type) {
			case *vfsapi.ReadAction:
				got = a.VFSID
			case *vfsapi.WriteAction:
				got = a.VFSID
			}
			if !got.IsEqual(want) {
				t.Errorf("the evaluator of %v was asked about %v", name, got)
			}
		}
	}
}

func TestTheManageActionIsNeverSentToAnEvaluator(t *testing.T) {
	f := newEvaluatorFixture(t)
	_, e := f.vfsWithEvaluator(t, true)
	stranger := astral.GenerateIdentity()

	if f.auth.Authorize(testContext(), manageAction(stranger)) {
		t.Fatal("a stranger may manage")
	}
	if f.mod.authorizeManage(testContext(), stranger) == nil {
		t.Fatal("a stranger passes the manage check")
	}
	if len(e.asked) != 0 || f.auth.questions() != 0 {
		t.Fatalf("a manage question reached an evaluator: %d questions", f.auth.questions())
	}
}

// TestTheEvaluatorIsAskedBeforeContractsAtEveryLevel: the auth module asks the handler, and so the evaluator,
// before it reads a contract, and again for the issuer at each level of a chain.
func TestTheEvaluatorIsAskedBeforeContractsAtEveryLevel(t *testing.T) {
	f := newEvaluatorFixture(t)
	vfs, e := f.vfsWithEvaluator(t, false)
	issuer, app := astral.GenerateIdentity(), astral.GenerateIdentity()
	authzIndex(t, f.auth, authzContract(t, issuer, app, &vfsapi.ReadAction{}, vfs))

	if f.auth.Authorize(testContext(), readAction(app, vfs)) {
		t.Fatal("a contract from an issuer nobody allows was honoured")
	}
	if len(e.actors) != 2 || !e.actors[0].IsEqual(app) || !e.actors[1].IsEqual(issuer) {
		t.Fatalf("the evaluator was asked about %v; want the app, then the issuer", e.actors)
	}

	e.allow = true
	e.asked, e.actors = nil, nil
	if !f.auth.Authorize(testContext(), readAction(app, vfs)) {
		t.Fatal("a caller the evaluator allows was refused")
	}
	if len(e.actors) != 1 || !e.actors[0].IsEqual(app) {
		t.Fatalf("the evaluator was asked about %v; want the app alone, before the contract", e.actors)
	}
}

// badEndpoints are endpoints an evaluator never takes: another transport, another op, arguments, a caller part,
// an empty target, and a value longer than mod.vfs.info carries.
func badEndpoints(app string) []string {
	return []string{
		"http://127.0.0.1:8081/authorize",
		"https://authz.example/authorize",
		"astral://" + app,
		"astral://" + app + ":vfs.statfs",
		"astral://localnode:vfs.set_evaluator",
		"astral://" + app + ":vfs.authorize?x=1",
		"astral://" + app + ":vfs.authorize?",
		"astral://" + app + "@" + app + ":vfs.authorize",
		"astral://localnode@telepathy:vfs.authorize",
		"astral://:vfs.authorize",
		"astral://" + app + ":vfs.authorize/x",
		app + ":vfs.authorize",
		"astral://" + strings.Repeat("a", 256) + ":vfs.authorize",
	}
}

// TestSetEvaluatorTakesTheAuthorizeOpOnly: the endpoint is astral://<identity-or-alias>:vfs.authorize; any other
// form answers invalid endpoint and leaves the field unchanged.
func TestSetEvaluatorTakesTheAuthorizeOpOnly(t *testing.T) {
	f := newEvaluatorFixture(t)
	vfs := astral.GenerateIdentity()
	createVFS(t, f.mod, vfs)
	app := astral.GenerateIdentity().String()

	const kept = "astral://telepathy:vfs.authorize"
	if err := f.mod.SetEvaluator(vfs, kept); err != nil {
		t.Fatal(err)
	}
	for _, endpoint := range badEndpoints(app) {
		if err := f.mod.SetEvaluator(vfs, endpoint); !errors.Is(err, vfsapi.ErrInvalidEndpoint) {
			t.Errorf("%.40q: %v; want invalid endpoint", endpoint, err)
		}
		if got := mustVolume(t, f.mod, vfs).Evaluator; got != kept {
			t.Fatalf("%.40q changed the evaluator to %q", endpoint, got)
		}
	}

	for _, endpoint := range []string{"astral://" + app + ":vfs.authorize", "astral://telepathy:vfs.authorize", ""} {
		if err := f.mod.SetEvaluator(vfs, endpoint); err != nil {
			t.Fatalf("%q: %v", endpoint, err)
		}
		if got := mustVolume(t, f.mod, vfs).Evaluator; got != endpoint {
			t.Fatalf("stored %q, want %q", got, endpoint)
		}
	}
}

// TestSetEvaluatorChecksExistenceBeforeTheEndpoint: an unknown VFS answers no such vfs whatever the endpoint.
func TestSetEvaluatorChecksExistenceBeforeTheEndpoint(t *testing.T) {
	f := newEvaluatorFixture(t)

	for _, endpoint := range []string{"astral://telepathy:vfs.authorize", "astral://telepathy:vfs.statfs"} {
		if err := f.mod.SetEvaluator(astral.GenerateIdentity(), endpoint); !errors.Is(err, vfsapi.ErrNoSuchVFS) {
			t.Fatalf("an unknown VFS with %q: %v; want no such vfs", endpoint, err)
		}
	}
}

// TestABadEndpointInTheRowIsNotAsked: a row holding an endpoint of any other form refuses without a question.
func TestABadEndpointInTheRowIsNotAsked(t *testing.T) {
	f := newEvaluatorFixture(t)
	vfs := astral.GenerateIdentity()
	vol := createVFS(t, f.mod, vfs)
	app := astral.GenerateIdentity().String()

	for _, endpoint := range badEndpoints(app) {
		// note: a fake that allows stands behind the bad value, so only the form check can refuse.
		f.auth.evaluators[endpoint] = &fakeEvaluator{allow: true}
		if err := f.mod.db.setVolumeEvaluator(vol.ID, endpoint); err != nil {
			t.Fatal(err)
		}
		if read, write := f.access(astral.GenerateIdentity(), vfs); read || write {
			t.Errorf("the row %.40q lets a caller read %v, write %v", endpoint, read, write)
		}
	}
	if n := f.auth.questions(); n != 0 {
		t.Fatalf("%d questions were put to endpoints of another form", n)
	}
}

func TestInfoShowsTheEvaluator(t *testing.T) {
	f := newEvaluatorFixture(t)
	vfs := astral.GenerateIdentity()
	createVFS(t, f.mod, vfs)

	info, err := f.mod.vfsInfo(mustVolume(t, f.mod, vfs))
	if err != nil {
		t.Fatal(err)
	}
	if info.Evaluator != "" {
		t.Fatalf("a new VFS shows the evaluator %q", info.Evaluator)
	}

	const endpoint = "astral://telepathy:vfs.authorize"
	if err = f.mod.SetEvaluator(vfs, endpoint); err != nil {
		t.Fatal(err)
	}
	if info, err = f.mod.vfsInfo(mustVolume(t, f.mod, vfs)); err != nil || info.Evaluator != endpoint {
		t.Fatalf("info shows %q, %v; want %q", info.Evaluator, err, endpoint)
	}
}

// TestCreateStartsWithAnEmptyEvaluator: vfs.create, as an administrator runs it, leaves the evaluator empty.
func TestCreateStartsWithAnEmptyEvaluator(t *testing.T) {
	f := newManageFixture(t)
	target := astral.GenerateIdentity()

	c := manageOpen(t, f.mod.OpCreate, f.mod.identity, "vfs.create?vfs="+target.String(), "")
	manageExpect[*astral.Ack](t, c)

	if got := mustVolume(t, f.mod, target).Evaluator; got != "" {
		t.Fatalf("vfs.create stored the evaluator %q", got)
	}
}
