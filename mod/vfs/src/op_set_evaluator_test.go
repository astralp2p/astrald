package vfs

import (
	"net/url"
	"testing"

	apiauth "github.com/astralp2p/astral-go/api/auth"
	vfsapi "github.com/astralp2p/astral-go/api/vfs"
	"github.com/astralp2p/astral-go/astral"
)

const testEndpoint = "astral://telepathy:vfs.authorize"

// setEvaluator runs vfs.set_evaluator as caller with the origin and returns its caller end.
func (f *manageFixture) setEvaluator(t *testing.T, caller, vfs *astral.Identity, endpoint, origin string) *manageCaller {
	t.Helper()

	qs := "vfs.set_evaluator?vfs=" + vfs.String() + "&endpoint=" + url.QueryEscape(endpoint)

	return manageOpen(t, f.mod.OpSetEvaluator, caller, qs, origin)
}

// TestSetEvaluatorIsAllowedToTheVFSRootCallersAndAdministrators: the VFS identity, the node's user, the node and a
// holder of the manage action set and clear the evaluator.
func TestSetEvaluatorIsAllowedToTheVFSRootCallersAndAdministrators(t *testing.T) {
	f := newManageFixture(t)
	vfs := astral.GenerateIdentity()
	createVFS(t, f.mod, vfs)

	callers := map[string]*astral.Identity{"vfs": vfs, "user": f.user, "node": f.mod.identity, "holder": f.holder}
	for name, caller := range callers {
		manageExpect[*astral.Ack](t, f.setEvaluator(t, caller, vfs, testEndpoint, ""))
		if got := mustVolume(t, f.mod, vfs).Evaluator; got != testEndpoint {
			t.Fatalf("%v set the evaluator to %q", name, got)
		}

		manageExpect[*astral.Ack](t, f.setEvaluator(t, caller, vfs, "", ""))
		if got := mustVolume(t, f.mod, vfs).Evaluator; got != "" {
			t.Fatalf("%v cleared the evaluator to %q", name, got)
		}
	}
}

// TestSetEvaluatorRefusesEveryoneElse: a stranger, the identity of another VFS, a holder of a read or write
// permit for the VFS and a holder of a constrained manage permit are refused, and the field is unchanged.
func TestSetEvaluatorRefusesEveryoneElse(t *testing.T) {
	f := newManageFixture(t)
	vfs, other := astral.GenerateIdentity(), astral.GenerateIdentity()
	createVFS(t, f.mod, vfs)
	createVFS(t, f.mod, other)
	if err := f.mod.SetEvaluator(vfs, testEndpoint); err != nil {
		t.Fatal(err)
	}

	reader, writer, constrained := astral.GenerateIdentity(), astral.GenerateIdentity(), astral.GenerateIdentity()
	f.auth.grant(reader, &apiauth.Permit{Action: astral.String8(vfsapi.ReadAction{}.ObjectType())})
	f.auth.grant(writer, &apiauth.Permit{Action: astral.String8(vfsapi.WriteAction{}.ObjectType())})
	f.auth.grant(constrained, managePermit(vfs))

	callers := map[string]*astral.Identity{
		"stranger": astral.GenerateIdentity(), "other vfs": other,
		"reader": reader, "writer": writer, "constrained": constrained,
	}
	for name, caller := range callers {
		t.Run(name, func(t *testing.T) {
			manageExpectError(t, f.setEvaluator(t, caller, vfs, "", ""), vfsapi.ErrNotPermitted)
		})
	}

	if got := mustVolume(t, f.mod, vfs).Evaluator; got != testEndpoint {
		t.Fatalf("a refused caller changed the evaluator to %q", got)
	}
}

// TestSetEvaluatorRefusesTheNetwork: a query from the network is refused for every caller, the VFS identity
// included.
func TestSetEvaluatorRefusesTheNetwork(t *testing.T) {
	f := newManageFixture(t)
	vfs := astral.GenerateIdentity()
	createVFS(t, f.mod, vfs)

	callers := map[string]*astral.Identity{"vfs": vfs, "user": f.user, "node": f.mod.identity, "holder": f.holder}
	for name, caller := range callers {
		t.Run(name, func(t *testing.T) {
			manageExpectError(t, f.setEvaluator(t, caller, vfs, testEndpoint, astral.OriginNetwork), vfsapi.ErrNotPermitted)
		})
	}

	if got := mustVolume(t, f.mod, vfs).Evaluator; got != "" {
		t.Fatalf("a query from the network set the evaluator to %q", got)
	}
}

// TestSetEvaluatorRefusesAnEndpointOfAnotherForm: an endpoint that is not astral://<identity-or-alias>:vfs.authorize
// answers invalid endpoint and leaves the field unchanged.
func TestSetEvaluatorRefusesAnEndpointOfAnotherForm(t *testing.T) {
	f := newManageFixture(t)
	vfs := astral.GenerateIdentity()
	createVFS(t, f.mod, vfs)
	manageExpect[*astral.Ack](t, f.setEvaluator(t, vfs, vfs, testEndpoint, ""))

	for _, endpoint := range badEndpoints(vfs.String()) {
		manageExpectError(t, f.setEvaluator(t, vfs, vfs, endpoint, ""), vfsapi.ErrInvalidEndpoint)
		if got := mustVolume(t, f.mod, vfs).Evaluator; got != testEndpoint {
			t.Fatalf("%.40q changed the evaluator to %q", endpoint, got)
		}
	}

	for _, endpoint := range []string{"astral://" + f.user.String() + ":vfs.authorize", "astral://localnode:vfs.authorize"} {
		manageExpect[*astral.Ack](t, f.setEvaluator(t, vfs, vfs, endpoint, ""))
		if got := mustVolume(t, f.mod, vfs).Evaluator; got != endpoint {
			t.Fatalf("stored %q, want %q", got, endpoint)
		}
	}
}

// TestSetEvaluatorChecksAuthorizationBeforeExistence: a stranger learns nothing about which VFSes exist, and an
// unknown VFS answers no such vfs before its endpoint is checked.
func TestSetEvaluatorChecksAuthorizationBeforeExistence(t *testing.T) {
	f := newManageFixture(t)
	missing := astral.GenerateIdentity()

	manageExpectError(t, f.setEvaluator(t, astral.GenerateIdentity(), missing, testEndpoint, ""), vfsapi.ErrNotPermitted)
	manageExpectError(t, f.setEvaluator(t, f.holder, missing, testEndpoint, ""), vfsapi.ErrNoSuchVFS)
	manageExpectError(t, f.setEvaluator(t, missing, missing, testEndpoint, ""), vfsapi.ErrNoSuchVFS)
	manageExpectError(t, f.setEvaluator(t, f.holder, missing, "astral://localnode:vfs.statfs", ""), vfsapi.ErrNoSuchVFS)
}
