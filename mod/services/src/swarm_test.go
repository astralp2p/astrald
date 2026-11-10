package services

import (
	"io"
	"slices"
	"sync/atomic"
	"testing"
	"time"

	"github.com/astralp2p/astral-go/api/auth"
	"github.com/astralp2p/astral-go/api/nodes"
	"github.com/astralp2p/astral-go/api/services"
	"github.com/astralp2p/astral-go/astral"
	alog "github.com/astralp2p/astral-go/astral/log"
	"github.com/astralp2p/astral-go/lib/query"
	"github.com/astralp2p/astral-go/lib/routing"
	apphostmod "github.com/astralp2p/astrald/mod/apphost"
	authmod "github.com/astralp2p/astrald/mod/auth"
	"github.com/astralp2p/astrald/mod/objects"
	"github.com/astralp2p/astrald/mod/services/src/coordinator"
	"github.com/astralp2p/astrald/mod/user"
)

// Two in-process nodes: node A's queries to node B reach B's discover op as
// link-origin queries from A, the way a link delivers them.

type swarmNode struct {
	id   *astral.Identity
	peer *Module
	op   any // replaces the peer's OpDiscover when set
	down atomic.Bool
}

func (n *swarmNode) Identity() *astral.Identity { return n.id }

func (n *swarmNode) RouteQuery(ctx *astral.Context, q *astral.InFlightQuery, w io.WriteCloser) (io.WriteCloser, error) {
	if n.down.Load() || n.peer == nil || !q.Target.IsEqual(n.peer.node.Identity()) {
		return query.RouteNotFound()
	}
	var fn any = n.peer.OpDiscover
	if n.op != nil {
		fn = n.op
	}
	op, err := routing.NewOp(fn)
	if err != nil {
		return nil, err
	}
	q.Extra.Set("origin", astral.OriginNetwork)
	return op.RouteQuery(ctx, q, w)
}

type swarmUser struct {
	user.Module
	swarm []*astral.Identity
	user  *astral.Identity // nil: the node has no user
}

// swarmApps is an apphost module whose swarm apps are fixed.
type swarmApps struct {
	apphostmod.Module
	apps []*astral.Identity
}

func (a *swarmApps) IsSwarmApp(_ *astral.Context, app *astral.Identity) bool {
	return slices.ContainsFunc(a.apps, app.IsEqual)
}

func (u *swarmUser) LocalSwarm() []*astral.Identity { return u.swarm }
func (u *swarmUser) Identity() *astral.Identity     { return u.user }

type swarmObjects struct {
	objects.Module
	pushes atomic.Int32
}

func (o *swarmObjects) Push(*astral.Context, *astral.Identity, astral.Object) error {
	o.pushes.Add(1)
	return nil
}

// swarmAuth answers Authorize per action type and finds one relay contract.
type swarmAuth struct {
	authmod.Module
	refuse map[string]bool // action type → refused
}

func (a *swarmAuth) Authorize(_ *astral.Context, action auth.ActionObject) bool {
	return !a.refuse[action.ObjectType()]
}

func (a *swarmAuth) SignedContracts() authmod.ContractQueryBuilder { return oneContract{} }

type oneContract struct{}

func (q oneContract) WithIssuer(*astral.Identity) authmod.ContractQueryBuilder  { return q }
func (q oneContract) WithSubject(*astral.Identity) authmod.ContractQueryBuilder { return q }
func (q oneContract) WithAction(...astral.Object) authmod.ContractQueryBuilder  { return q }
func (oneContract) Find(*astral.Context) ([]*auth.SignedContract, error) {
	return []*auth.SignedContract{{Contract: &auth.Contract{}}}, nil
}

type swarm struct {
	a, b     *Module
	nodeA    *swarmNode
	authA    *swarmAuth
	authB    *swarmAuth
	objectsA *swarmObjects
	apps     *swarmApps
}

func newSwarm() *swarm {
	cfg := coordinator.DefaultConfig()
	cfg.InitialBudget = time.Second
	s := &swarm{
		nodeA:    &swarmNode{id: astral.GenerateIdentity()},
		authA:    &swarmAuth{refuse: map[string]bool{}},
		authB:    &swarmAuth{refuse: map[string]bool{}},
		objectsA: &swarmObjects{},
	}
	nodeB := &swarmNode{id: astral.GenerateIdentity()}
	members := &swarmUser{swarm: []*astral.Identity{s.nodeA.id, nodeB.id}}
	s.apps = &swarmApps{}
	s.a = &Module{Deps: Deps{Auth: s.authA, Apphost: s.apps, Objects: s.objectsA, User: members}, node: s.nodeA, log: alog.New(s.nodeA.id), coord: coordinator.New(cfg)}
	s.b = &Module{Deps: Deps{Auth: s.authB, Apphost: s.apps, Objects: &swarmObjects{}, User: members}, node: nodeB, log: alog.New(nodeB.id), coord: coordinator.New(cfg)}
	s.nodeA.peer = s.b
	return s
}

// An app on A discovering with reach=swarm sees A's and B's providers; B's
// provider evaluates the app, not node A.
func TestSwarmDiscoveryCarriesTheAppToAMember(t *testing.T) {
	s := newSwarm()
	_, localP := advertise(t, s.a, "player", available)
	asked := make(chan *astral.Identity, 4)
	_, remoteP := advertise(t, s.b, "player", func(a *services.Ask) *services.Update {
		asked <- a.CallerID
		return &services.Update{Available: true}
	})
	app := astral.GenerateIdentity()

	c := open(t, s.a.OpDiscover, app, "services.discover?services=player&reach=swarm", false)
	seen := map[string]bool{}
	for range 2 {
		seen[expect[*services.Update](t, c).ProviderID.String()] = true
	}
	expect[*astral.EOS](t, c)
	expectEnd(t, c)

	if !seen[localP.String()] || !seen[remoteP.String()] {
		t.Fatalf("saw %v; want the local and the member's provider", seen)
	}
	if caller := <-asked; !caller.IsEqual(app) {
		t.Fatalf("the member's provider evaluated %v; want the app %v", caller, app)
	}
	if s.objectsA.pushes.Load() == 0 {
		t.Fatal("node A did not push the app's relay contract before asking the member")
	}
}

// Losing the member's provider reaches the app's follow as a removal; the
// local offering stays.
func TestSwarmFollowCarriesMemberRemovals(t *testing.T) {
	s := newSwarm()
	advertise(t, s.a, "player", available)
	remote, remoteP := advertise(t, s.b, "player", available)

	c := open(t, s.a.OpDiscover, astral.GenerateIdentity(), "services.discover?services=player&reach=swarm&follow=true", false)
	expect[*services.Update](t, c)
	expect[*services.Update](t, c)
	expect[*astral.EOS](t, c)

	remote.close()
	r := expect[*services.Removed](t, c)
	if len(r.Offerings) != 1 || !r.Offerings[0].ProviderID.IsEqual(remoteP) {
		t.Fatalf("removed %+v; want the member's provider only", r.Offerings)
	}
}

// A member that refuses to act for the app, or cannot be reached, leaves the
// app with an incomplete outcome rather than a false complete one.
func TestSwarmMemberFailureIsIncomplete(t *testing.T) {
	for name, fail := range map[string]func(*swarm){
		"refuses relay": func(s *swarm) { s.authB.refuse[nodes.RelayForAction{}.ObjectType()] = true },
		"unreachable":   func(s *swarm) { s.nodeA.down.Store(true) },
	} {
		t.Run(name, func(t *testing.T) {
			s := newSwarm()
			fail(s)
			c := open(t, s.a.OpDiscover, astral.GenerateIdentity(), "services.discover?services=player&reach=swarm", false)
			in := expect[*services.Incomplete](t, c)
			if len(in.Services) != 1 || in.Services[0] != "player" {
				t.Fatalf("incomplete %+v; want player", in.Services)
			}
			expect[*astral.EOS](t, c)
		})
	}
}

// A member out of reach at first is asked again when a link to it appears.
func TestSwarmRestoresAMemberOnLink(t *testing.T) {
	s := newSwarm()
	_, remoteP := advertise(t, s.b, "player", available)
	s.nodeA.down.Store(true)

	c := open(t, s.a.OpDiscover, astral.GenerateIdentity(), "services.discover?services=player&reach=swarm&follow=true", false)
	expect[*services.Incomplete](t, c)
	expect[*astral.EOS](t, c)

	s.nodeA.down.Store(false)
	s.a.links.notify(s.b.node.Identity())
	if u := expect[*services.Update](t, c); !u.ProviderID.IsEqual(remoteP) {
		t.Fatalf("got %v; want the member's provider after the link", u.ProviderID)
	}
}

// The host carries a discovery for an app without permits and filters what
// the member returns: the member's provider is asked, nothing reaches the app.
func TestSwarmHostFiltersWhatItCarries(t *testing.T) {
	s := newSwarm()
	asked := make(chan struct{}, 1)
	advertise(t, s.b, "player", func(*services.Ask) *services.Update {
		asked <- struct{}{}
		return &services.Update{Available: true}
	})
	s.authA.refuse[services.ServiceDiscoveryAction{}.ObjectType()] = true

	c := open(t, s.a.OpDiscover, astral.GenerateIdentity(), "services.discover?services=player&reach=swarm", false)
	if ng := expect[*services.NotGranted](t, c); len(ng.Services) != 1 || ng.Services[0] != "player" {
		t.Fatalf("not granted %v; want player", ng.Services)
	}
	expect[*astral.EOS](t, c)
	expectEnd(t, c)
	select {
	case <-asked:
	case <-time.After(testWait):
		t.Fatal("the member's provider was not asked")
	}
}

// Admission rules on reach and for.
func TestDiscoverReachAndForAdmission(t *testing.T) {
	app := astral.GenerateIdentity().String()
	for name, tc := range map[string]struct {
		query   string
		network bool
	}{
		"unknown reach":           {"services.discover?services=player&reach=network", false},
		"for from a local caller": {"services.discover?services=player&for=" + app, false},
		"for carried further":     {"services.discover?services=player&reach=swarm&for=" + app, true},
		"swarm reach over a link": {"services.discover?services=player&reach=swarm", true},
		"invalid for":             {"services.discover?services=player&for=nobody", true},
	} {
		t.Run(name, func(t *testing.T) {
			s := newSwarm()
			c := open(t, s.a.OpDiscover, astral.GenerateIdentity(), tc.query, tc.network)
			expect[*astral.ErrorMessage](t, c)
		})
	}
}

// Node identity cannot advertise: the query is answered auth.ErrNotPermitted.
func TestAdvertiseRejectsTheNodeIdentity(t *testing.T) {
	mod, _ := wireModule(true)
	op, err := routing.NewOp(mod.OpAdvertise)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := astral.NewContext(nil).WithTimeout(testWait)
	defer cancel()
	nodeID := mod.node.Identity()
	w := newRecordingWriter()
	_, err = op.RouteQuery(ctx, astral.Launch(query.New(nodeID, nodeID, "services.advertise?services=nat", nil)), w)
	if err != nil {
		t.Fatalf("advertise as the node: route: %v", err)
	}
	if got, want := w.refusal(t), auth.ErrNotPermitted.Error(); got != want {
		t.Fatalf("advertise as the node: answered %q; want %q", got, want)
	}
}
