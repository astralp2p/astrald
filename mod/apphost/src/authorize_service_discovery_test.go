package apphost

import (
	"bytes"
	"io"
	"testing"
	"time"

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

// TestGrantOpRecordsConstraintsFromTheChannel: with constrained=true the op
// reads one bundle and records it as the permit's constraints.
func TestGrantOpRecordsConstraintsFromTheChannel(t *testing.T) {
	mod := testGrantModule(t)
	mod.Auth = &recordingAuth{verdict: true}
	app, node := astral.GenerateIdentity(), astral.GenerateIdentity()
	action := services.ServiceDiscoveryAction{}.ObjectType()

	w := newRecordingWriter()
	in := routeWithInput(t, mod.OpGrant, astral.GenerateIdentity(),
		"apphost.grant?identity="+app.String()+"&action="+action+"&constrained=true", w)
	if err := channel.New(in).Send(playerScope()); err != nil {
		t.Fatalf("send constraints: %v", err)
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

// TestGrantOpRefusesAConstrainedGrantWithoutABundle: an input that is not a
// bundle records nothing.
func TestGrantOpRefusesAConstrainedGrantWithoutABundle(t *testing.T) {
	mod := testGrantModule(t)
	mod.Auth = &recordingAuth{verdict: true}
	app := astral.GenerateIdentity()
	action := services.ServiceDiscoveryAction{}.ObjectType()

	w := newRecordingWriter()
	in := routeWithInput(t, mod.OpGrant, astral.GenerateIdentity(),
		"apphost.grant?identity="+app.String()+"&action="+action+"&constrained=true", w)
	if err := channel.New(in).Send(astral.NewString8("player")); err != nil {
		t.Fatalf("send: %v", err)
	}
	waitAdminManageAppsAnswer(t, w)

	grants, err := mod.Grants(app)
	if err != nil {
		t.Fatalf("grants: %v", err)
	}
	if len(grants) != 0 {
		t.Fatalf("recorded %d grants from a non-bundle input; want 0", len(grants))
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
