package sys

import (
	"errors"
	"strings"
	"sync"
	"testing"

	"github.com/astralp2p/astral-go/api/apphost"
	"github.com/astralp2p/astral-go/api/auth"
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

// TestRegisterManagerAppoints: a fresh node provisions the manager with both
// permit lists in its contract, records the appointment and answers the
// manager's token.
func TestRegisterManagerAppoints(t *testing.T) {
	mod, apps, _ := freshModule()
	ctx := astral.NewContext(nil)

	grant := &auth.Permit{Action: "mod.user.see_swarm_action"}
	contract := &auth.Permit{Action: "mod.auth.see_objects_action"}
	w := registerManager(t, mod, &sysapi.RegisterManagerRequest{
		Name:            "control-plane",
		GrantPermits:    []*auth.Permit{grant},
		ContractPermits: []*auth.Permit{contract},
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
	if len(apps.permits) != 2 || apps.permits[0].Action != grant.Action || apps.permits[1].Action != contract.Action {
		t.Fatalf("the manager's contract holds %v; want both permit lists", apps.permits)
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

// TestRegisterManagerRefusesPolicyBindings: a request that binds a policy is
// refused and writes nothing.
func TestRegisterManagerRefusesPolicyBindings(t *testing.T) {
	mod, apps, _ := freshModule()
	w := registerManager(t, mod, &sysapi.RegisterManagerRequest{Policies: []*sysapi.PolicyBinding{
		{Policy: "example_policy", Operation: "x.y"},
	}})
	if got := w.refusal(t); got != errPoliciesRemoved.Error() {
		t.Fatalf("refusal %q; want %q", got, errPoliciesRemoved)
	}
	if !mod.Manager().IsZero() || apps.issued() != 0 {
		t.Fatal("a refused request appointed or issued a token")
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
