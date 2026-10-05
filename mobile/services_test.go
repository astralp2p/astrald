package mobile

import (
	"testing"
	"time"

	"github.com/astralp2p/astral-go/api/auth"
	"github.com/astralp2p/astral-go/api/services"
	"github.com/astralp2p/astral-go/astral"
	servicesmod "github.com/astralp2p/astrald/mod/services"
)

type fakeServices struct {
	names []string
	eval  servicesmod.Evaluator
}

func (f *fakeServices) RegisterNative(names []string, e servicesmod.Evaluator) (servicesmod.Registration, error) {
	f.names, f.eval = names, e
	return fakeRegistration{}, nil
}

type fakeRegistration struct{}

func (fakeRegistration) Changed() {}
func (fakeRegistration) Close()   {}

type playerEvaluator struct{ offered string }

func (p playerEvaluator) Available(caller, _ string) bool { return caller != p.offered }
func (playerEvaluator) Operations(string) string          { return "player.play, player.pause" }

func TestRegisterServiceAdvertisesThroughTheServicesModule(t *testing.T) {
	refused := astral.GenerateIdentity()
	mod := &fakeServices{}
	reg, err := registerService(mod, "player", playerEvaluator{offered: refused.String()})
	if err != nil || reg == nil {
		t.Fatalf("register: %v", err)
	}
	if len(mod.names) != 1 || mod.names[0] != "player" {
		t.Fatalf("registered %v; want player", mod.names)
	}

	u := mod.eval.Evaluate(astral.GenerateIdentity(), "player")
	if u == nil || !bool(u.Available) || u.Info == nil {
		t.Fatalf("offering %+v; want available with info", u)
	}
	ops, ok := u.Info.Objects()[0].(*services.OperationsList)
	if !ok || len(ops.Operations) != 2 || ops.Operations[1] != "player.pause" {
		t.Fatalf("operations %+v; want player.play and player.pause", u.Info.Objects())
	}

	if u := mod.eval.Evaluate(refused, "player"); u != nil {
		t.Fatalf("offered %+v to a caller the evaluator refuses", u)
	}
}

func TestRegisterServiceRejectsInvalidInput(t *testing.T) {
	if _, err := registerService(&fakeServices{}, "a,,b", playerEvaluator{}); err == nil {
		t.Fatal("accepted an invalid name list")
	}
	if _, err := registerService(&fakeServices{}, "player", nil); err == nil {
		t.Fatal("accepted a nil evaluator")
	}
	if _, err := NewNode().RegisterService("player", playerEvaluator{}); err == nil {
		t.Fatal("registered on a node that is not running")
	}
}

type grantRecorder struct {
	identity *astral.Identity
	permit   *auth.Permit
	expires  *time.Time
}

func (g *grantRecorder) Grant(identity *astral.Identity, permit *auth.Permit, expiresAt *time.Time) error {
	g.identity, g.permit, g.expires = identity, permit, expiresAt
	return nil
}

// The grant lets the app discover exactly the named services, on any node,
// until revoked.
func TestGrantDiscoveryScopesTheNamedServices(t *testing.T) {
	app, node := astral.GenerateIdentity(), astral.GenerateIdentity()
	g := &grantRecorder{}
	if err := grantDiscovery(g, app, []string{"contacts-backend"}); err != nil {
		t.Fatal(err)
	}
	if !g.identity.IsEqual(app) || g.expires != nil {
		t.Fatalf("granted to %v until %v; want the app, unexpiring", g.identity, g.expires)
	}
	allows := func(service string) bool {
		return g.permit.Allows(&services.ServiceDiscoveryAction{
			Action: auth.NewAction(app), Service: astral.String8(service), NodeID: node,
		})
	}
	if !allows("contacts-backend") {
		t.Fatal("the grant does not cover contacts-backend")
	}
	if allows("player") {
		t.Fatal("the grant covers a service it does not name")
	}
}

func TestGrantServiceDiscoveryNeedsARunningNode(t *testing.T) {
	if err := NewNode().GrantServiceDiscovery(astral.GenerateIdentity().String(), "contacts-backend"); err == nil {
		t.Fatal("granted on a node that is not running")
	}
}
