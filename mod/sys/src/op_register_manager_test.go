package sys

import (
	"errors"
	"strings"
	"sync"
	"testing"

	"github.com/astralp2p/astral-go/api/apphost"
	"github.com/astralp2p/astral-go/api/policy"
	sysapi "github.com/astralp2p/astral-go/api/sys"
	"github.com/astralp2p/astral-go/api/user"
	"github.com/astralp2p/astral-go/astral"
)

var errProvisionRefused = errors.New("provision refused")

// registerManager routes sys.register_manager with req and returns the
// caller's end of the connection.
func registerManager(t *testing.T, mod *Module, req *sysapi.RegisterManagerRequest) *recordingWriter {
	t.Helper()
	return routeSending(t, mod.OpRegisterManager, localQuery(sysapi.MethodRegisterManager), req)
}

// TestRegisterManagerAppoints: a fresh node provisions the manager, stores a
// delegate naming the manager for each binding, clears every other delegate,
// records the appointment and answers the manager's token.
func TestRegisterManagerAppoints(t *testing.T) {
	mod, apps, u := freshModule()
	ctx := astral.NewContext(nil)

	// a delegate a failed attempt left behind
	leftover := &policy.Delegate{Identity: astral.GenerateIdentity(), Operation: "old.decide"}
	apps.SetPolicyDelegate(ctx, sysapi.PolicyGrantRequest, leftover)

	w := registerManager(t, mod, &sysapi.RegisterManagerRequest{
		Name: "control-plane",
		Policies: []*sysapi.PolicyBinding{
			{Policy: sysapi.PolicyAppRegister, Operation: "cp.approve_app"},
			{Policy: sysapi.PolicySwarmJoin, Operation: "cp.approve_join"},
		},
	})

	token, ok := w.answer(t).(*apphost.AccessToken)
	if !ok {
		t.Fatal("answer is not an access token")
	}
	manager := mod.Manager()
	if !manager.IsEqual(token.Identity) {
		t.Fatalf("manager = %v; want the token's identity %v", manager, token.Identity)
	}
	if got := mod.NodeState(ctx); got != user.StateUnclaimed {
		t.Fatalf("state = %v; want unclaimed", got)
	}

	if d := apps.policies[sysapi.PolicyAppRegister]; d.IsZero() || !d.Identity.IsEqual(manager) || d.Operation != "cp.approve_app" {
		t.Fatalf("app_register delegate = %+v; want the manager's cp.approve_app", d)
	}
	if d := apps.policies[sysapi.PolicyGrantRequest]; d != nil {
		t.Fatal("grant_request kept the leftover delegate")
	}
	if j := u.policies[sysapi.PolicySwarmJoin]; j.IsZero() || !j.Identity.IsEqual(manager) || j.Operation != "cp.approve_join" {
		t.Fatalf("swarm_join delegate = %+v; want the manager's cp.approve_join", j)
	}
	for _, name := range []string{sysapi.PolicySwarmInvite, sysapi.PolicyContractAccept} {
		if d, set := u.policies[name]; !set || d != nil {
			t.Fatalf("%v delegate = %+v, written %v; want cleared", name, d, set)
		}
	}
}

// TestRegisterManagerRefusesANodeThatIsNotFresh: an appointed, a claimed and
// a formerly owned node answer ErrNotFresh and issue no token.
func TestRegisterManagerRefusesANodeThatIsNotFresh(t *testing.T) {
	ctx := astral.NewContext(nil)

	appointed, appointedApps, _ := freshModule()
	first := astral.GenerateIdentity()
	appointed.appointment.Manager.Set(ctx, first)

	claimed, claimedApps, _ := freshModule()
	claimed.User = &stubUser{id: astral.GenerateIdentity(), ready: closedChan()}

	owned, ownedApps, _ := freshModule()
	owned.User = &stubUser{ready: closedChan(), owned: true}

	for name, c := range map[string]struct {
		mod  *Module
		apps *fakeApphost
	}{"appointed": {appointed, appointedApps}, "claimed": {claimed, claimedApps}, "owned before": {owned, ownedApps}} {
		w := registerManager(t, c.mod, &sysapi.RegisterManagerRequest{})
		if got := w.refusal(t); got != sysapi.ErrNotFresh.Error() {
			t.Fatalf("%v: refusal %q; want %q", name, got, sysapi.ErrNotFresh)
		}
		if n := c.apps.issued(); n != 0 {
			t.Fatalf("%v: issued %d tokens; want none", name, n)
		}
	}
	if !appointed.Manager().IsEqual(first) {
		t.Fatal("a refused appointment replaced the manager")
	}
}

// TestRegisterManagerAppointsOnce: of concurrent appointments exactly one
// commits.
func TestRegisterManagerAppointsOnce(t *testing.T) {
	mod, apps, _ := freshModule()

	const n = 8
	var wg sync.WaitGroup
	answers := make(chan astral.Object, n)
	for range n {
		wg.Add(1)
		go func() {
			defer wg.Done()
			answers <- registerManager(t, mod, &sysapi.RegisterManagerRequest{}).answer(t)
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
	if got := apps.issued(); got != 1 {
		t.Fatalf("issued %d tokens; want one", got)
	}
}

// TestRegisterManagerRefusesInvalidBindings: a refused request writes nothing.
func TestRegisterManagerRefusesInvalidBindings(t *testing.T) {
	for name, b := range map[string][]*sysapi.PolicyBinding{
		"unknown policy": {{Policy: "approve_everything", Operation: "x.y"}},
		"no operation":   {{Policy: sysapi.PolicyAppRegister}},
		"bound twice": {
			{Policy: sysapi.PolicyAppRegister, Operation: "x.a"},
			{Policy: sysapi.PolicyAppRegister, Operation: "x.b"},
		},
		"timeout above the bound": {{Policy: sysapi.PolicyAppRegister, Operation: "x.y", Timeout: astral.Duration(2 * policy.MaxTimeout)}},
	} {
		mod, apps, _ := freshModule()
		w := registerManager(t, mod, &sysapi.RegisterManagerRequest{Policies: b})
		if got := w.refusal(t); got == "" {
			t.Fatalf("%v: answered no refusal", name)
		}
		if !mod.Manager().IsZero() || apps.issued() != 0 {
			t.Fatalf("%v: a refused request appointed or issued a token", name)
		}
	}
}

// TestRegisterManagerRefusesAUserPolicyWithoutTheUserModule: a node without
// the user module refuses a user policy binding.
func TestRegisterManagerRefusesAUserPolicyWithoutTheUserModule(t *testing.T) {
	mod, apps, _ := freshModule()
	mod.User = nil
	w := registerManager(t, mod, &sysapi.RegisterManagerRequest{Policies: []*sysapi.PolicyBinding{
		{Policy: sysapi.PolicySwarmJoin, Operation: "x.y"},
	}})
	if got := w.refusal(t); !strings.Contains(got, "user module is not loaded") {
		t.Fatalf("refusal %q; want the user module refusal", got)
	}
	if apps.issued() != 0 {
		t.Fatal("a refused request issued a token")
	}
}

// TestRegisterManagerLeavesTheNodeFreshOnFailure: a failure before the record
// leaves the node fresh, and a retry appoints.
func TestRegisterManagerLeavesTheNodeFreshOnFailure(t *testing.T) {
	mod, apps, _ := freshModule()

	apps.err = errProvisionRefused
	w := registerManager(t, mod, &sysapi.RegisterManagerRequest{})
	if got := w.refusal(t); !strings.Contains(got, errProvisionRefused.Error()) {
		t.Fatalf("refusal %q; want %q", got, errProvisionRefused)
	}
	if got := mod.NodeState(astral.NewContext(nil)); got != user.StateFresh {
		t.Fatalf("state after failure = %v; want fresh", got)
	}

	apps.err = nil
	if _, ok := registerManager(t, mod, &sysapi.RegisterManagerRequest{}).answer(t).(*apphost.AccessToken); !ok {
		t.Fatal("the retry answered no token")
	}
	if mod.Manager().IsZero() {
		t.Fatal("the retry appointed no manager")
	}
}

// TestRegisterManagerRefusesTheNetworkOrigin: an identity minted over a link
// cannot become the manager.
func TestRegisterManagerRefusesTheNetworkOrigin(t *testing.T) {
	mod, apps, _ := freshModule()
	q := localQuery(sysapi.MethodRegisterManager)
	q.Extra.Set("origin", astral.OriginNetwork)

	w := routeSending(t, mod.OpRegisterManager, q, &sysapi.RegisterManagerRequest{})
	if got := w.refusal(t); got == "" || !mod.Manager().IsZero() || apps.issued() != 0 {
		t.Fatalf("refusal %q, manager %v; want a refusal and no manager", got, mod.Manager())
	}
}
