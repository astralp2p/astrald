package apphost

import (
	"bytes"
	"errors"
	"strings"
	"sync"
	"testing"

	"github.com/astralp2p/astral-go/api/apphost"
	"github.com/astralp2p/astral-go/api/policy"
	"github.com/astralp2p/astral-go/api/user"
	"github.com/astralp2p/astral-go/astral"
	"github.com/astralp2p/astral-go/astral/channel"
)

var errIndexRefused = errors.New("index refused")

// managerRegistrar is a registrar on a fresh node with a user module stub.
func managerRegistrar(t *testing.T) (*Module, *stubUser) {
	t.Helper()
	mod := serveAppsRegistrar(t)
	u := &stubUser{ready: closedChan()}
	mod.User = u
	return mod, u
}

// registerManager routes apphost.register_manager with req and waits for the
// answer.
func registerManager(t *testing.T, mod *Module, req *apphost.RegisterManagerRequest) *recordingWriter {
	t.Helper()
	w := newRecordingWriter()
	err := routeSending(t, mod.OpRegisterManager, astral.GenerateIdentity(), "apphost.register_manager", req, w)
	if err != nil {
		t.Fatalf("apphost.register_manager rejected: %v", err)
	}
	awaitServeAppsClose(t, w)
	return w
}

// answer decodes the one object the op answered.
func (w *recordingWriter) answer(t *testing.T) astral.Object {
	t.Helper()
	w.mu.Lock()
	defer w.mu.Unlock()
	obj, err := channel.NewReceiver(bytes.NewReader(w.buf.Bytes())).Receive()
	if err != nil {
		t.Fatalf("decode answer: %v", err)
	}
	return obj
}

// TestRegisterManagerAppoints: a fresh node provisions the manager, stores a
// delegate naming the manager for each binding, clears every other delegate,
// records the appointment and answers the manager's token.
func TestRegisterManagerAppoints(t *testing.T) {
	mod, u := managerRegistrar(t)
	ctx := astral.NewContext(nil)

	// a delegate a failed attempt left behind
	leftover := &policy.Delegate{Identity: astral.GenerateIdentity(), Operation: "old.decide"}
	if err := mod.policy.GrantRequest.Set(ctx, leftover); err != nil {
		t.Fatal(err)
	}

	w := registerManager(t, mod, &apphost.RegisterManagerRequest{
		Name: "control-plane",
		Policies: []*apphost.PolicyBinding{
			{Policy: apphost.PolicyAppRegister, Operation: "cp.approve_app"},
			{Policy: apphost.PolicySwarmJoin, Operation: "cp.approve_join"},
		},
	})

	token, ok := w.answer(t).(*apphost.AccessToken)
	if !ok {
		t.Fatalf("answer is not an access token")
	}
	manager := mod.Manager()
	if !manager.IsEqual(token.Identity) {
		t.Fatalf("manager = %v; want the token's identity %v", manager, token.Identity)
	}
	if got := mod.NodeState(ctx); got != user.StateUnclaimed {
		t.Fatalf("state = %v; want unclaimed", got)
	}

	d := mod.policy.AppRegister.Get()
	if d.IsZero() || !d.Identity.IsEqual(manager) || d.Operation != "cp.approve_app" {
		t.Fatalf("app_register delegate = %+v; want the manager's cp.approve_app", d)
	}
	if !mod.policy.GrantRequest.Get().IsZero() {
		t.Fatal("grant_request kept the leftover delegate")
	}
	if j := u.policies[apphost.PolicySwarmJoin]; j.IsZero() || !j.Identity.IsEqual(manager) || j.Operation != "cp.approve_join" {
		t.Fatalf("swarm_join delegate = %+v; want the manager's cp.approve_join", j)
	}
	for _, name := range []string{apphost.PolicySwarmInvite, apphost.PolicyContractAccept} {
		if d, set := u.policies[name]; !set || d != nil {
			t.Fatalf("%v delegate = %+v, written %v; want cleared", name, d, set)
		}
	}
}

// TestRegisterManagerRefusesANodeThatIsNotFresh: an appointed or claimed node
// answers ErrNotFresh and issues no token.
func TestRegisterManagerRefusesANodeThatIsNotFresh(t *testing.T) {
	ctx := astral.NewContext(nil)

	appointed, _ := managerRegistrar(t)
	first := astral.GenerateIdentity()
	if err := appointed.appointment.Manager.Set(ctx, first); err != nil {
		t.Fatal(err)
	}

	claimed, _ := managerRegistrar(t)
	claimed.User = &stubUser{id: astral.GenerateIdentity(), ready: closedChan()}

	owned, _ := managerRegistrar(t)
	owned.User = &stubUser{ready: closedChan(), owned: true}

	for name, mod := range map[string]*Module{"appointed": appointed, "claimed": claimed, "owned before": owned} {
		w := registerManager(t, mod, &apphost.RegisterManagerRequest{})
		if got := w.refusal(t); got != apphost.ErrNotFresh.Error() {
			t.Fatalf("%v: refusal %q; want %q", name, got, apphost.ErrNotFresh)
		}
		if tokens := issuedTokens(t, mod); len(tokens) != 0 {
			t.Fatalf("%v: issued %d tokens; want none", name, len(tokens))
		}
	}
	if !appointed.Manager().IsEqual(first) {
		t.Fatal("a refused appointment replaced the manager")
	}
}

// TestRegisterManagerAppointsOnce: of concurrent appointments exactly one
// commits.
func TestRegisterManagerAppointsOnce(t *testing.T) {
	mod, _ := managerRegistrar(t)

	const n = 8
	var wg sync.WaitGroup
	answers := make(chan astral.Object, n)
	for range n {
		wg.Add(1)
		go func() {
			defer wg.Done()
			answers <- registerManager(t, mod, &apphost.RegisterManagerRequest{}).answer(t)
		}()
	}
	wg.Wait()
	close(answers)

	var tokens []*apphost.AccessToken
	for a := range answers {
		if token, ok := a.(*apphost.AccessToken); ok {
			tokens = append(tokens, token)
		}
	}
	if len(tokens) != 1 {
		t.Fatalf("%d appointments answered a token; want one", len(tokens))
	}
	if !mod.Manager().IsEqual(tokens[0].Identity) {
		t.Fatal("the manager is not the identity of the one token")
	}
	if got := issuedTokens(t, mod); len(got) != 1 {
		t.Fatalf("issued %d tokens; want one", len(got))
	}
}

// TestRegisterManagerRefusesInvalidBindings: a refused request writes nothing.
func TestRegisterManagerRefusesInvalidBindings(t *testing.T) {
	for name, b := range map[string][]*apphost.PolicyBinding{
		"unknown policy": {{Policy: "approve_everything", Operation: "x.y"}},
		"no operation":   {{Policy: apphost.PolicyAppRegister}},
		"bound twice": {
			{Policy: apphost.PolicyAppRegister, Operation: "x.a"},
			{Policy: apphost.PolicyAppRegister, Operation: "x.b"},
		},
		"timeout above the bound": {{Policy: apphost.PolicyAppRegister, Operation: "x.y", Timeout: astral.Duration(2 * policy.MaxTimeout)}},
	} {
		mod, _ := managerRegistrar(t)
		w := registerManager(t, mod, &apphost.RegisterManagerRequest{Policies: b})
		if got := w.refusal(t); got == "" {
			t.Fatalf("%v: answered no refusal", name)
		}
		if !mod.Manager().IsZero() || len(issuedTokens(t, mod)) != 0 {
			t.Fatalf("%v: a refused request appointed or issued a token", name)
		}
	}
}

// TestRegisterManagerLeavesTheNodeFreshOnFailure: a failure before the record
// leaves the node fresh, and a retry appoints.
func TestRegisterManagerLeavesTheNodeFreshOnFailure(t *testing.T) {
	steps := &registerSteps{}
	mod, _ := managerRegistrar(t)
	mod.Auth = registerAuth{steps: steps}
	mod.Crypto = registerKeys{steps: steps}
	mod.Objects = registerStore{steps: steps}

	steps.indexErr = errIndexRefused
	w := registerManager(t, mod, &apphost.RegisterManagerRequest{})
	if got := w.refusal(t); !strings.Contains(got, errIndexRefused.Error()) {
		t.Fatalf("refusal %q; want %q", got, errIndexRefused)
	}
	if got := mod.NodeState(astral.NewContext(nil)); got != user.StateFresh {
		t.Fatalf("state after failure = %v; want fresh", got)
	}

	steps.indexErr = nil
	if _, ok := registerManager(t, mod, &apphost.RegisterManagerRequest{}).answer(t).(*apphost.AccessToken); !ok {
		t.Fatal("the retry answered no token")
	}
	if mod.Manager().IsZero() {
		t.Fatal("the retry appointed no manager")
	}
}

// TestRegisterManagerRefusesTheNetworkOrigin: an identity minted over a link
// cannot become the manager.
func TestRegisterManagerRefusesTheNetworkOrigin(t *testing.T) {
	mod, _ := managerRegistrar(t)
	caller := astral.GenerateIdentity()
	q := astral.Launch(&astral.Query{Nonce: astral.NewNonce(), Caller: caller, Target: caller, QueryString: "apphost.register_manager"})
	q.Extra.Set("origin", astral.OriginNetwork)

	w := newRecordingWriter()
	if err := routeQuerySending(t, mod.OpRegisterManager, q, &apphost.RegisterManagerRequest{}, w); err != nil {
		t.Fatalf("rejected: %v", err)
	}
	if got := w.refusal(t); got == "" || !mod.Manager().IsZero() {
		t.Fatalf("refusal %q, manager %v; want a refusal and no manager", got, mod.Manager())
	}
}
