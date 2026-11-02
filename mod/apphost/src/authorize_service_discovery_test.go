package apphost

import (
	"bytes"
	"io"
	"strings"
	"testing"
	"time"

	"github.com/astralp2p/astral-go/api/apphost"
	"github.com/astralp2p/astral-go/api/auth"
	"github.com/astralp2p/astral-go/api/services"
	"github.com/astralp2p/astral-go/astral"
	"github.com/astralp2p/astral-go/astral/channel"
	"github.com/astralp2p/astral-go/lib/query"
	"github.com/astralp2p/astral-go/lib/routing"
)

func discoveryAction(actor, node *astral.Identity, service astral.String8) *services.ServiceDiscoveryAction {
	return &services.ServiceDiscoveryAction{Action: auth.NewAction(actor), Service: service, NodeID: node}
}

func playerScope() *astral.Bundle {
	cs := astral.NewBundle()
	scope := &services.DiscoveryScope{Rules: []*services.DiscoveryRule{{Services: []astral.String8{"player"}}}}
	if err := cs.Append(scope); err != nil {
		panic(err)
	}
	return cs
}

// TestServiceDiscoveryGrantNarrowsByScope: a grant with a scope covers the
// services it names and no other; an unconstrained grant covers nothing.
func TestServiceDiscoveryGrantNarrowsByScope(t *testing.T) {
	mod := testGrantModule(t)
	app, node := astral.GenerateIdentity(), astral.GenerateIdentity()
	action := astral.String8(services.ServiceDiscoveryAction{}.ObjectType())

	if err := mod.Grant(app, &auth.Permit{Action: action}, nil); err != nil {
		t.Fatalf("grant: %v", err)
	}
	if mod.AuthorizeServiceDiscovery(nil, discoveryAction(app, node, "player")) {
		t.Fatal("an unconstrained discovery grant must cover nothing")
	}

	if err := mod.Grant(app, &auth.Permit{Action: action, Constraints: playerScope()}, nil); err != nil {
		t.Fatalf("grant: %v", err)
	}
	if !mod.AuthorizeServiceDiscovery(nil, discoveryAction(app, node, "player")) {
		t.Fatal("a grant scoped to player must cover player")
	}
	if mod.AuthorizeServiceDiscovery(nil, discoveryAction(app, node, "bitcoin-wallet")) {
		t.Fatal("a grant scoped to player must not cover bitcoin-wallet")
	}
	if mod.AuthorizeServiceDiscovery(nil, discoveryAction(astral.GenerateIdentity(), node, "player")) {
		t.Fatal("another identity must not inherit the grant")
	}
}

// TestDiscoveryGrantsAddUp: single-service grants given one by one together
// cover each service they name and no other.
func TestDiscoveryGrantsAddUp(t *testing.T) {
	mod := testGrantModule(t)
	app, node := astral.GenerateIdentity(), astral.GenerateIdentity()
	action := astral.String8(services.ServiceDiscoveryAction{}.ObjectType())

	for _, service := range []astral.String8{"player", "wallet"} {
		cs := astral.NewBundle()
		scope := &services.DiscoveryScope{Rules: []*services.DiscoveryRule{{Services: []astral.String8{service}}}}
		if err := cs.Append(scope); err != nil {
			t.Fatalf("scope: %v", err)
		}
		if err := mod.Grant(app, &auth.Permit{Action: action, Constraints: cs}, nil); err != nil {
			t.Fatalf("grant %v: %v", service, err)
		}
	}

	for _, service := range []astral.String8{"player", "wallet"} {
		if !mod.AuthorizeServiceDiscovery(nil, discoveryAction(app, node, service)) {
			t.Fatalf("the grants must cover %v", service)
		}
	}
	if mod.AuthorizeServiceDiscovery(nil, discoveryAction(app, node, "radio")) {
		t.Fatal("the grants must not cover radio")
	}
}

// TestGrantOpRecordsConstraintsFromTheChannel: the op records each permit of
// the grant_request with its constraints.
func TestGrantOpRecordsConstraintsFromTheChannel(t *testing.T) {
	mod := testGrantModule(t)
	mod.Auth = &recordingAuth{verdict: true}
	app, node := astral.GenerateIdentity(), astral.GenerateIdentity()

	w := newRecordingWriter()
	in := routeWithInput(t, mod.OpGrant, astral.GenerateIdentity(), "apphost.grant", w)
	req := &apphost.GrantRequest{Identity: app, Permits: []*auth.Permit{discoveryPermit("player")}}
	if err := channel.New(in).Send(req); err != nil {
		t.Fatalf("send request: %v", err)
	}
	waitAdminManageAppsAnswer(t, w)

	grants, err := mod.Grants(app)
	if err != nil {
		t.Fatalf("grants: %v", err)
	}
	if len(grants) != 1 || grants[0].Constraints == nil || len(grants[0].Constraints.Objects()) != 1 {
		t.Fatalf("recorded grants %+v; want one permit with one constraint", grants)
	}
	if !mod.AuthorizeServiceDiscovery(nil, discoveryAction(app, node, "player")) {
		t.Fatal("the granted scope must cover player")
	}
}

// TestGrantOpRefusesAnInputOtherThanAGrantRequest: an input that is not a
// grant_request records nothing.
func TestGrantOpRefusesAnInputOtherThanAGrantRequest(t *testing.T) {
	mod := testGrantModule(t)
	mod.Auth = &recordingAuth{verdict: true}

	w := newRecordingWriter()
	in := routeWithInput(t, mod.OpGrant, astral.GenerateIdentity(), "apphost.grant", w)
	if err := channel.New(in).Send(playerScope()); err != nil {
		t.Fatalf("send: %v", err)
	}
	waitAdminManageAppsAnswer(t, w)

	var n int64
	if err := mod.db.Model(&dbGrant{}).Count(&n).Error; err != nil {
		t.Fatalf("count grants: %v", err)
	}
	if n != 0 {
		t.Fatalf("recorded %d grants from a bundle input; want 0", n)
	}
}

// TestGrantOpRefusesUnscopedDiscovery: a discovery permit without exactly one
// scope is refused with an invalid permit error naming the action, and
// nothing is written.
func TestGrantOpRefusesUnscopedDiscovery(t *testing.T) {
	action := astral.String8(services.ServiceDiscoveryAction{}.ObjectType())
	twoScopes := discoveryPermit("player")
	if err := twoScopes.Constraints.Append(discoveryPermit("wallet").Constraints.Objects()[0]); err != nil {
		t.Fatalf("scope: %v", err)
	}

	for name, p := range map[string]*auth.Permit{
		"no constraints": {Action: action},
		"empty bundle":   {Action: action, Constraints: astral.NewBundle()},
		"two scopes":     twoScopes,
	} {
		t.Run(name, func(t *testing.T) {
			mod := testGrantModule(t)
			mod.Auth = &recordingAuth{verdict: true}
			app := astral.GenerateIdentity()

			w := newRecordingWriter()
			req := &apphost.GrantRequest{Identity: app, Permits: []*auth.Permit{p}}
			if err := routeSending(t, mod.OpGrant, astral.GenerateIdentity(), "apphost.grant", req, w); err != nil {
				t.Fatalf("route: %v", err)
			}
			if got, want := readReply(t, w), "invalid permit: "+string(action)+": "; !strings.HasPrefix(got, want) {
				t.Fatalf("answered %q; want %q…", got, want)
			}
			if held := heldActions(t, mod, app); len(held) != 0 {
				t.Fatalf("a refused grant wrote %q", held)
			}
		})
	}
}

// routeWithInput dispatches one query and returns the stream that feeds the
// op's input.
func routeWithInput(t *testing.T, fn any, caller *astral.Identity, queryString string, w io.WriteCloser) io.ReadWriter {
	t.Helper()

	op, err := routing.NewOp(fn)
	if err != nil {
		t.Fatalf("new op: %v", err)
	}

	ctx, cancel := astral.NewContext(nil).WithTimeout(10 * time.Second)
	t.Cleanup(cancel)

	target, err := op.RouteQuery(ctx, astral.Launch(query.New(caller, caller, queryString, nil)), w)
	if err != nil {
		t.Fatalf("route: %v", err)
	}
	return struct {
		io.Reader
		io.Writer
	}{&bytes.Buffer{}, target}
}
