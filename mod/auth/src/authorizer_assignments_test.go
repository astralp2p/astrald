package auth

import (
	"errors"
	"sync"
	"testing"

	"github.com/astralp2p/astral-go/api/auth"
	"github.com/astralp2p/astral-go/api/tree"
	"github.com/astralp2p/astral-go/astral"
	"github.com/astralp2p/astral-go/lib/routing"
	authmod "github.com/astralp2p/astrald/mod/auth"
	"github.com/astralp2p/astrald/mod/dir"
)

// leafNode is a tree node holding one object, standing in for persistence that
// outlives a module.
type leafNode struct {
	mu      sync.Mutex
	obj     astral.Object
	failSet bool
}

func (n *leafNode) Get(ctx *astral.Context, follow bool) (<-chan astral.Object, error) {
	n.mu.Lock()
	obj := n.obj
	n.mu.Unlock()
	if obj == nil {
		obj = &astral.Nil{}
	}

	ch := make(chan astral.Object, 1)
	ch <- obj
	if !follow {
		close(ch)
	}
	return ch, nil
}

func (n *leafNode) Set(_ *astral.Context, obj astral.Object) error {
	n.mu.Lock()
	defer n.mu.Unlock()
	if n.failSet {
		return errors.New("disk full")
	}
	n.obj = obj
	return nil
}

func (n *leafNode) Delete(*astral.Context) error { return tree.ErrUnsupported }

func (n *leafNode) Sub(*astral.Context) (map[string]tree.Node, error) { return nil, nil }

func (n *leafNode) Create(*astral.Context, string) (tree.Node, error) {
	return nil, tree.ErrUnsupported
}

// namedDir resolves names from a map.
type namedDir struct {
	dir.Module
	ids map[string]*astral.Identity
}

func (d *namedDir) ResolveIdentity(name string) (*astral.Identity, error) {
	if id, ok := d.ids[name]; ok {
		return id, nil
	}
	return astral.ParseIdentity(name)
}

// boundModule is a test module whose assignments persist in node.
func boundModule(t *testing.T, node *leafNode, configured []*auth.AuthorizerAssignment) *Module {
	t.Helper()

	mod := testModule(t)
	mod.Dir = &namedDir{ids: map[string]*astral.Identity{}}
	mod.authorizers.NoLocal = true

	ctx := astral.NewContext(nil)
	if err := mod.authorizers.Bind(ctx, node); err != nil {
		t.Fatalf("bind: %v", err)
	}
	if err := mod.restoreAuthorizers(ctx, configured); err != nil {
		t.Fatalf("restore: %v", err)
	}

	return mod
}

func endpointOf(t *testing.T, mod *Module, action string, actor *astral.Identity) string {
	t.Helper()

	h, ok := mod.external.Get(externalKey(action, actor))
	if !ok {
		return ""
	}
	return h.config.Endpoint
}

// TestActorAssignmentPrecedesActionWide: the actor's own authority answers for
// it alone, and only it is asked; other actors reach the action-wide one.
func TestActorAssignmentPrecedesActionWide(t *testing.T) {
	mod := testModule(t)
	ctx := astral.NewContext(nil)
	player, other := astral.GenerateIdentity(), astral.GenerateIdentity()

	wide := &stubAsk{answer: true}
	own := &stubAsk{answer: false}
	mod.AddExternalAuthorizer("test.action", nil, ExternalConfig{}, wide)
	mod.AddExternalAuthorizer("test.action", player, ExternalConfig{}, own)

	if mod.Authorize(ctx, action(player)) {
		t.Fatal("the actor's refusing authority was overruled")
	}
	if wide.askedAbout(player) {
		t.Fatal("the action-wide authority was asked about an actor with its own")
	}
	if !mod.Authorize(ctx, action(other)) {
		t.Fatal("another actor did not reach the action-wide authority")
	}

	mod.RemoveExternalAuthorizer("test.action", player)
	if !mod.Authorize(ctx, action(player)) {
		t.Fatal("removing the actor's assignment did not restore the action-wide one")
	}
}

// TestAssignmentsSurviveReload: what one module sets, the next restores.
func TestAssignmentsSurviveReload(t *testing.T) {
	node := &leafNode{}
	ctx := astral.NewContext(nil)
	player := astral.GenerateIdentity()

	first := boundModule(t, node, nil)
	for _, a := range []*auth.AuthorizerAssignment{
		{Action: "test.action", Endpoint: "astral://telepathy:auth.authorize"},
		{Action: "test.action", Actor: player, Endpoint: "http://media/evaluate", TokenValue: "s3cr3t"},
	} {
		if err := first.SetAuthorizer(ctx, a); err != nil {
			t.Fatalf("set: %v", err)
		}
	}

	second := boundModule(t, node, nil)
	if got := endpointOf(t, second, "test.action", nil); got != "astral://telepathy:auth.authorize" {
		t.Fatalf("action-wide endpoint after reload: %q", got)
	}
	if got := endpointOf(t, second, "test.action", player); got != "http://media/evaluate" {
		t.Fatalf("actor endpoint after reload: %q", got)
	}

	if err := second.RemoveAuthorizer(ctx, "test.action", player); err != nil {
		t.Fatalf("remove: %v", err)
	}

	third := boundModule(t, node, nil)
	if got := endpointOf(t, third, "test.action", player); got != "" {
		t.Fatalf("removed assignment returned after reload: %q", got)
	}
}

// TestConfigurationWinsOnEveryStart: a configured action-wide assignment
// replaces the persisted one of the same key; others persist.
func TestConfigurationWinsOnEveryStart(t *testing.T) {
	node := &leafNode{}
	ctx := astral.NewContext(nil)

	first := boundModule(t, node, nil)
	for _, a := range []*auth.AuthorizerAssignment{
		{Action: "test.action", Endpoint: "astral://runtime:auth.authorize"},
		{Action: "test.other_action", Endpoint: "astral://runtime:auth.authorize"},
	} {
		if err := first.SetAuthorizer(ctx, a); err != nil {
			t.Fatalf("set: %v", err)
		}
	}

	configured := []*auth.AuthorizerAssignment{{Action: "test.action", Endpoint: "astral://yaml:auth.authorize"}}
	second := boundModule(t, node, configured)

	if got := endpointOf(t, second, "test.action", nil); got != "astral://yaml:auth.authorize" {
		t.Fatalf("configured action: %q", got)
	}
	if got := endpointOf(t, second, "test.other_action", nil); got != "astral://runtime:auth.authorize" {
		t.Fatalf("unconfigured action: %q", got)
	}
	if n := len(second.assignments()); n != 2 {
		t.Fatalf("persisted %v assignments, want 2", n)
	}
}

// TestSetAuthorizerFailsWhenPersistenceFails: an unsaved assignment is not
// applied.
func TestSetAuthorizerFailsWhenPersistenceFails(t *testing.T) {
	node := &leafNode{}
	mod := boundModule(t, node, nil)
	node.failSet = true

	err := mod.SetAuthorizer(astral.NewContext(nil), &auth.AuthorizerAssignment{
		Action: "test.action", Endpoint: "astral://telepathy:auth.authorize",
	})
	if err == nil {
		t.Fatal("a failed save was acknowledged")
	}
	if _, ok := mod.external.Get("test.action"); ok {
		t.Fatal("an unsaved assignment was applied")
	}
}

func TestSetAuthorizerRefusesTheActorAsItsAuthority(t *testing.T) {
	mod := boundModule(t, &leafNode{}, nil)
	media := astral.GenerateIdentity()
	mod.Dir.(*namedDir).ids["media"] = media

	err := mod.SetAuthorizer(astral.NewContext(nil), &auth.AuthorizerAssignment{
		Action: "test.action", Actor: media, Endpoint: "astral://media:auth.evaluate",
	})
	if err == nil {
		t.Fatal("an actor was assigned as its own authority")
	}
}

// TestAdminOpsAdmitOnlyLocalAdministrators: a network caller is refused
// whatever it holds, and a local caller without AdminManageApps is refused.
func TestAdminOpsAdmitOnlyLocalAdministrators(t *testing.T) {
	mod := testModule(t)
	ctx := astral.NewContext(nil)
	admin, stranger := astral.GenerateIdentity(), astral.GenerateIdentity()

	mod.Add(authmod.Func[*auth.AdminManageAppsAction](func(_ *astral.Context, a *auth.AdminManageAppsAction) bool {
		return a.Actor().IsEqual(admin)
	}))

	for _, tc := range []struct {
		name   string
		caller *astral.Identity
		origin string
		admit  bool
	}{
		{"local admin", admin, astral.OriginLocal, true},
		{"network admin", admin, astral.OriginNetwork, false},
		{"local stranger", stranger, astral.OriginLocal, false},
	} {
		q := routing.NewIncomingQuery(astral.NewQuery(tc.caller, astral.GenerateIdentity(), "auth.set_authorizer"), nil, tc.origin)
		if got := mod.admitAdmin(ctx, q); got != tc.admit {
			t.Errorf("%s: admitted %v, want %v", tc.name, got, tc.admit)
		}
	}
}

// TestListOmitsTheTokenValue: a listed assignment never carries the token.
func TestListOmitsTheTokenValue(t *testing.T) {
	mod := boundModule(t, &leafNode{}, nil)
	err := mod.SetAuthorizer(astral.NewContext(nil), &auth.AuthorizerAssignment{
		Action: "test.action", Endpoint: "http://authz/authorize", TokenValue: "s3cr3t",
	})
	if err != nil {
		t.Fatalf("set: %v", err)
	}

	listed := mod.listedAssignments()
	if len(listed) != 1 || listed[0].TokenValue != "" {
		t.Fatalf("listed: %+v", listed)
	}
}
