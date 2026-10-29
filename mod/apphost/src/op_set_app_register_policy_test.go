package apphost

import (
	"testing"
	"time"

	"github.com/astralp2p/astral-go/api/auth"
	"github.com/astralp2p/astral-go/api/policy"
	"github.com/astralp2p/astral-go/astral"
	"github.com/astralp2p/astral-go/astral/log"
)

// policySetter is a module whose setter authorizes every caller when admin is
// set and resolves hex identities. Its delegate value is unbound, so a write
// lands in memory, as a bound value's does on a successful save.
func policySetter(admin bool) *Module {
	return &Module{
		Deps:   Deps{Auth: &recordingAuth{verdict: admin}, Dir: &namingDir{}},
		config: defaultConfig,
		node:   &serveAppsNode{id: astral.GenerateIdentity()},
		log:    log.New(nil),
	}
}

func setPolicy(t *testing.T, mod *Module, args string) (*recordingWriter, error) {
	t.Helper()

	w := newRecordingWriter()
	err := route(t, mod.OpSetAppRegisterPolicy, astral.GenerateIdentity(), "apphost.set_app_register_policy"+args, w)
	return w, err
}

// A caller without AdminManageApps is answered auth.ErrNotPermitted before
// anything is written.
func TestSetAppRegisterPolicyRequiresAdminManageApps(t *testing.T) {
	mod := policySetter(false)

	w, err := setPolicy(t, mod, "?identity="+astral.GenerateIdentity().String()+"&operation=control.decide")
	if err != nil {
		t.Fatalf("route: %v", err)
	}
	if got, want := w.refusal(t), auth.ErrNotPermitted.Error(); got != want {
		t.Fatalf("answered %q; want %q", got, want)
	}
	if !mod.policy.AppRegister.Get().IsZero() {
		t.Fatal("a rejected setter wrote a delegate")
	}
}

// Identity, operation, and timeout are persisted together, and the getter
// then selects the delegated policy.
func TestSetAppRegisterPolicyPersistsTheDelegate(t *testing.T) {
	mod := policySetter(true)
	id := astral.GenerateIdentity()

	w, err := setPolicy(t, mod, "?identity="+id.String()+"&operation=control.decide&timeout=5m")
	if err != nil {
		t.Fatalf("set: %v", err)
	}
	awaitServeAppsClose(t, w)

	d := mod.policy.AppRegister.Get()
	if d.IsZero() || !d.Identity.IsEqual(id) || d.Operation != "control.decide" || time.Duration(d.Timeout) != 5*time.Minute {
		t.Fatalf("persisted %+v, want the delegate as set", d)
	}
}

// A timeout above the node's bound is refused before persistence.
func TestSetAppRegisterPolicyRefusesATimeoutAboveTheBound(t *testing.T) {
	mod := policySetter(true)

	w, err := setPolicy(t, mod, "?identity="+astral.GenerateIdentity().String()+"&operation=control.decide&timeout=11m")
	if err != nil {
		t.Fatalf("set: %v", err)
	}
	awaitServeAppsClose(t, w)

	if !mod.policy.AppRegister.Get().IsZero() {
		t.Fatalf("a timeout above %v was persisted", policy.MaxTimeout)
	}
}

// The node cannot be its own delegate.
func TestSetAppRegisterPolicyRefusesTheNodeItself(t *testing.T) {
	mod := policySetter(true)

	w, err := setPolicy(t, mod, "?identity="+mod.node.Identity().String()+"&operation=apphost.register")
	if err != nil {
		t.Fatalf("set: %v", err)
	}
	awaitServeAppsClose(t, w)

	if !mod.policy.AppRegister.Get().IsZero() {
		t.Fatal("the node was persisted as its own delegate")
	}
}

// Naming one of identity and operation alone is an error, not a default.
func TestSetAppRegisterPolicyRefusesHalfADelegate(t *testing.T) {
	mod := policySetter(true)

	w, err := setPolicy(t, mod, "?operation=control.decide")
	if err != nil {
		t.Fatalf("set: %v", err)
	}
	awaitServeAppsClose(t, w)

	if !mod.policy.AppRegister.Get().IsZero() {
		t.Fatal("an operation without an identity was persisted")
	}
}

// Both empty clears the delegate, which restores accept-all.
func TestSetAppRegisterPolicyClearsTheDelegate(t *testing.T) {
	mod := policySetter(true)
	d := &policy.Delegate{Identity: astral.GenerateIdentity(), Operation: "control.decide"}
	if err := mod.policy.AppRegister.Set(astral.NewContext(nil), d); err != nil {
		t.Fatalf("seed: %v", err)
	}

	w, err := setPolicy(t, mod, "")
	if err != nil {
		t.Fatalf("clear: %v", err)
	}
	awaitServeAppsClose(t, w)

	if !mod.policy.AppRegister.Get().IsZero() {
		t.Fatal("an empty set did not clear the delegate")
	}
	if !registerAsk(astral.NewContext(nil), mod.GetRegisterAppPolicy()) {
		t.Fatal("clearing the delegate did not restore accept-all")
	}
}
