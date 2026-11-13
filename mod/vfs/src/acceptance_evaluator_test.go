package vfs_test

import (
	"fmt"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	apphostapi "github.com/astralp2p/astral-go/api/apphost"
	apphostapiclient "github.com/astralp2p/astral-go/api/apphost/client"
	vfsapi "github.com/astralp2p/astral-go/api/vfs"
	"github.com/astralp2p/astral-go/astral"
	"github.com/astralp2p/astral-go/astral/channel"
	"github.com/astralp2p/astral-go/lib/apps"
	"github.com/astralp2p/astral-go/lib/query"
	"github.com/astralp2p/astral-go/lib/routing"
)

// note: an evaluator test runs an app that serves vfs.authorize through lib/apps, as an evaluating app does, and
// the node asks it through the auth module's astral asker.

// evaluatorAnswer is what the evaluator app answers.
type evaluatorAnswer int32

const (
	answerYes evaluatorAnswer = iota
	answerNo
	answerError
	answerLate   // after the auth module's default timeout
	answerSilent // accepts the query and never reads the question
)

// evaluatorApp serves vfs.authorize and records each question.
type evaluatorApp struct {
	answer atomic.Int32
	mu     sync.Mutex
	asked  []astral.Object
}

type evaluatorArgs struct {
	In  string
	Out string
}

func (e *evaluatorApp) authorize(ctx *astral.Context, q *routing.IncomingQuery, args evaluatorArgs) error {
	ch := q.Accept(channel.WithFormats(args.In, args.Out))
	defer ch.Close()

	if evaluatorAnswer(e.answer.Load()) == answerSilent {
		select {
		case <-ctx.Done():
		case <-time.After(acceptanceWait):
		}
		return nil
	}

	action, err := ch.Receive()
	if err != nil {
		return err
	}
	e.mu.Lock()
	e.asked = append(e.asked, action)
	e.mu.Unlock()

	switch evaluatorAnswer(e.answer.Load()) {
	case answerYes:
		return ch.Send(&astral.Ack{})
	case answerNo:
		return ch.Send(astral.NewError("no"))
	case answerLate:
		time.Sleep(time.Second)
		return ch.Send(&astral.Ack{})
	}

	return ch.Send(astral.Err(vfsapi.ErrNotPermitted))
}

// questions returns the VFS of each question the app was asked, and the type of the action.
func (e *evaluatorApp) questions() []string {
	e.mu.Lock()
	defer e.mu.Unlock()

	var list []string
	for _, a := range e.asked {
		switch a := a.(type) {
		case *vfsapi.ReadAction:
			list = append(list, "read "+a.VFSID.String())
		case *vfsapi.WriteAction:
			list = append(list, "write "+a.VFSID.String())
		default:
			list = append(list, a.ObjectType())
		}
	}

	return list
}

// registerAppWith registers a new app through apphost.register with req.
func (a *acceptanceNode) registerAppWith(req *apphostapi.RegisterRequest) *acceptanceApp {
	a.t.Helper()

	ch := a.mustChannel(a.tokenless(), "apphost.register", nil)
	if err := ch.Send(req); err != nil {
		a.t.Fatalf("send the register request: %v", err)
	}

	answer, err := ch.Receive()
	if err != nil {
		a.t.Fatalf("apphost.register: %v", err)
	}
	token, ok := answer.(*apphostapi.AccessToken)
	if !ok {
		a.t.Fatalf("apphost.register answered %#v", answer)
	}

	return &acceptanceApp{id: token.Identity, token: string(token.Token), client: a.withToken(string(token.Token))}
}

// serveEvaluator serves vfs.authorize as app until it returns its stop function or the test ends.
func (a *acceptanceNode) serveEvaluator(app *acceptanceApp) (*evaluatorApp, func()) {
	a.t.Helper()

	e := &evaluatorApp{}
	router := routing.NewOpRouter()
	op, err := routing.NewOp(e.authorize)
	if err != nil {
		a.t.Fatalf("new op: %v", err)
	}
	if err = router.AddOp(vfsapi.MethodAuthorize, op); err != nil {
		a.t.Fatalf("add op: %v", err)
	}

	ctx, cancel := a.ctx.WithCancel()
	reg := apps.NewAppRegistrar(ctx, apps.WithClient(apphostapiclient.New(a.nodeID(), app.client)))
	address := fmt.Sprintf("memu:vfs-evaluator-%d", acceptanceListeners.Add(1))

	done := make(chan error, 1)
	go func() { done <- apps.ServeWith(ctx, router, reg, apps.WithHandlerAddress(address)) }()

	var stopped atomic.Bool
	stop := func() {
		if stopped.CompareAndSwap(false, true) {
			cancel()
			<-done
		}
	}
	a.t.Cleanup(stop)

	select {
	case <-reg.Ready():
	case err := <-done:
		a.t.Fatalf("serve the evaluator: %v", err)
	case <-time.After(acceptanceWait):
		a.t.Fatal("the evaluator did not register")
	}

	return e, stop
}

// evaluatorOf returns the evaluator vfs.statfs shows for the VFS of app.
func (a *acceptanceNode) evaluatorOf(app *acceptanceApp) string {
	a.t.Helper()

	answer, err := a.call(app.client, "vfs.statfs", nil)
	info, ok := answer.(*vfsapi.Info)
	if err != nil || !ok {
		a.t.Fatalf("vfs.statfs answered %#v, %v", answer, err)
	}

	return string(info.Evaluator)
}

// mayRead reports whether vfs.ls of the root of the VFS of owner succeeds for caller.
func (a *acceptanceNode) mayRead(caller, owner *acceptanceApp) bool {
	answer, err := a.call(caller.client, "vfs.ls", query.Args{"vfs": owner.id, "path": "/"})
	if err != nil {
		a.t.Fatalf("vfs.ls: %v", err)
	}
	_, refused := answer.(astral.Error)

	return !refused
}

// mayWrite reports whether vfs.mkdir in the VFS of owner succeeds for caller.
func (a *acceptanceNode) mayWrite(caller, owner *acceptanceApp, path string) bool {
	answer, err := a.call(caller.client, "vfs.mkdir", query.Args{"vfs": owner.id, "path": path})
	if err != nil {
		a.t.Fatalf("vfs.mkdir: %v", err)
	}
	_, refused := answer.(astral.Error)

	return !refused
}

// TestAcceptanceEvaluatorDecidesForRefusedCallers: an app registered with the defaults evaluates its own VFS; a
// caller without a permit reads and writes it when the app answers yes, and is refused when it answers no, with
// an error, or too late.
func TestAcceptanceEvaluatorDecidesForRefusedCallers(t *testing.T) {
	a := startAcceptanceNode(t)
	a.claim()
	owner := a.registerApp()
	caller := a.registerAppWith(&apphostapi.RegisterRequest{Evaluator: astral.NewBool(false)})

	if got, want := a.evaluatorOf(owner), "astral://"+owner.id.String()+":vfs.authorize"; got != want {
		t.Fatalf("the registered app's evaluator is %q; want %q", got, want)
	}

	e, _ := a.serveEvaluator(owner)

	e.answer.Store(int32(answerYes))
	if !a.mayRead(caller, owner) || !a.mayWrite(caller, owner, "/by-caller") {
		t.Fatal("a caller the evaluator allows was refused")
	}
	want := []string{"read " + owner.id.String(), "write " + owner.id.String()}
	if got := e.questions(); fmt.Sprint(got) != fmt.Sprint(want) {
		t.Fatalf("the evaluator was asked %v; want %v", got, want)
	}

	for name, answer := range map[string]evaluatorAnswer{"no": answerNo, "error": answerError, "late": answerLate} {
		e.answer.Store(int32(answer))
		if a.mayRead(caller, owner) || a.mayWrite(caller, owner, "/"+name) {
			t.Errorf("an evaluator answering %v let the caller in", name)
		}
	}
}

// TestAcceptanceEvaluatorIsNotAskedAboutTheOwnerOrTheUser: the owner and the node's user are allowed by the rules,
// so the evaluator hears nothing about them; an evaluator that is not running refuses a caller.
func TestAcceptanceEvaluatorIsNotAskedAboutTheOwnerOrTheUser(t *testing.T) {
	a := startAcceptanceNode(t)
	user := a.claim()
	owner := a.registerApp()
	caller := a.registerAppWith(&apphostapi.RegisterRequest{Evaluator: astral.NewBool(false)})

	e, stop := a.serveEvaluator(owner)
	e.answer.Store(int32(answerNo))

	if answer, err := a.call(a.as(user), "vfs.ls", query.Args{"vfs": owner.id, "path": "/"}); err != nil {
		t.Fatalf("vfs.ls as the user: %v", err)
	} else if _, refused := answer.(astral.Error); refused {
		t.Fatalf("the user was refused: %v", answer)
	}
	if !a.mayRead(owner, owner) || !a.mayWrite(owner, owner, "/by-owner") {
		t.Fatal("the owner was refused on its own VFS")
	}
	if got := e.questions(); len(got) != 0 {
		t.Fatalf("the evaluator was asked %v about the owner and the user", got)
	}

	stop()
	if a.mayRead(caller, owner) || a.mayWrite(caller, owner, "/stopped") {
		t.Fatal("an evaluator that is not running let the caller in")
	}
}

// TestAcceptanceEvaluatorIsAskedAboutItsOwnVFSOnly: the evaluator of one app hears nothing about another VFS, and
// a VFS whose evaluator is cleared refuses a caller without a question.
func TestAcceptanceEvaluatorIsAskedAboutItsOwnVFSOnly(t *testing.T) {
	a := startAcceptanceNode(t)
	a.claim()
	owner, other := a.registerApp(), a.registerApp()
	caller := a.registerAppWith(&apphostapi.RegisterRequest{Evaluator: astral.NewBool(false)})

	e, _ := a.serveEvaluator(owner)
	e.answer.Store(int32(answerYes))

	if a.mayRead(caller, other) || a.mayWrite(caller, other, "/x") {
		t.Fatal("a caller was let into a VFS whose evaluator is not running")
	}
	if a.mayRead(owner, other) {
		t.Fatal("the evaluator of one VFS was let into another")
	}
	if got := e.questions(); len(got) != 0 {
		t.Fatalf("the evaluator was asked about another VFS: %v", got)
	}

	// note: the owner clears its own evaluator; the VFS then refuses as it did before evaluators.
	answer, err := a.call(owner.client, "vfs.set_evaluator", query.Args{"vfs": owner.id, "endpoint": ""})
	if _, ok := answer.(*astral.Ack); err != nil || !ok {
		t.Fatalf("vfs.set_evaluator answered %#v, %v; want ack", answer, err)
	}
	if a.mayRead(caller, owner) || a.mayWrite(caller, owner, "/x") {
		t.Fatal("a VFS without an evaluator let a caller without a permit in")
	}
	if got := e.questions(); len(got) != 0 {
		t.Fatalf("a VFS without an evaluator put questions %v", got)
	}

	answer, err = a.call(caller.client, "vfs.set_evaluator",
		query.Args{"vfs": owner.id, "endpoint": "astral://" + caller.id.String() + ":vfs.authorize"})
	if e, isErr := answer.(astral.Error); err != nil || !isErr || e.Error() != vfsapi.ErrNotPermitted.Error() {
		t.Fatalf("vfs.set_evaluator by a stranger answered %#v, %v; want not permitted", answer, err)
	}
}

// TestAcceptanceRegisterWithoutAVFS: apphost.register with the VFS switch off leaves no VFS.
func TestAcceptanceRegisterWithoutAVFS(t *testing.T) {
	a := startAcceptanceNode(t)
	a.claim()
	app := a.registerAppWith(&apphostapi.RegisterRequest{Vfs: astral.NewBool(false)})

	answer, err := a.call(app.client, "vfs.statfs", nil)
	if e, isErr := answer.(astral.Error); err != nil || !isErr || e.Error() != vfsapi.ErrNoSuchVFS.Error() {
		t.Fatalf("vfs.statfs answered %#v, %v; want no such vfs", answer, err)
	}
}
