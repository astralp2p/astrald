package services

import (
	"slices"
	"sort"
	"sync"
	"testing"
	"time"

	"github.com/astralp2p/astral-go/api/auth"
	"github.com/astralp2p/astral-go/api/services"
	"github.com/astralp2p/astral-go/astral"
	"github.com/astralp2p/astral-go/lib/routing"
	authmod "github.com/astralp2p/astrald/mod/auth"
)

// permitAuth grants every action except service discovery, which it grants to
// the swarm's nodes and to the (actor, service, node) permits it holds.
type permitAuth struct {
	authmod.Module
	nodes []*astral.Identity

	mu      sync.Mutex
	permits map[string]bool
}

func permitKeyOf(actor *astral.Identity, service string, node *astral.Identity) string {
	return actor.String() + "|" + service + "|" + node.String()
}

func (a *permitAuth) grant(actor *astral.Identity, service string, node *astral.Identity) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.permits[permitKeyOf(actor, service, node)] = true
}

func (a *permitAuth) Authorize(_ *astral.Context, action auth.ActionObject) bool {
	d, ok := action.(*services.ServiceDiscoveryAction)
	if !ok {
		return true
	}
	if slices.ContainsFunc(a.nodes, d.Actor().IsEqual) {
		return true
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.permits[permitKeyOf(d.Actor(), string(d.Service), d.NodeID)]
}

func (a *permitAuth) SignedContracts() authmod.ContractQueryBuilder { return oneContract{} }

// visibilitySwarm is two nodes, A and B, with one swarm app and the permits
// the test grants.
type visibilitySwarm struct {
	*swarm
	auth *permitAuth
	app  *astral.Identity
}

func newVisibilitySwarm() *visibilitySwarm {
	s := newSwarm()
	pa := &permitAuth{nodes: []*astral.Identity{s.a.node.Identity(), s.b.node.Identity()}, permits: map[string]bool{}}
	s.a.Auth, s.b.Auth = pa, pa
	app := astral.GenerateIdentity()
	s.apps.apps = []*astral.Identity{app}
	return &visibilitySwarm{swarm: s, auth: pa, app: app}
}

// collect reads updates until eos and returns the names they offer, sorted,
// and the services not granted.
func collect(t *testing.T, c *peer) (offered, notGranted []string) {
	t.Helper()
	for {
		obj, err := recv(t, c)
		if err != nil {
			t.Fatalf("receive: %v", err)
		}
		switch o := obj.(type) {
		case *services.NotGranted:
			if offered != nil {
				t.Fatal("services.not_granted after an update")
			}
			for _, s := range o.Services {
				notGranted = append(notGranted, string(s))
			}
		case *services.Update:
			offered = append(offered, string(o.Name))
		case *astral.EOS:
			sort.Strings(offered)
			return offered, notGranted
		default:
			t.Fatalf("received %s %v", obj.ObjectType(), obj)
		}
	}
}

func equal(got []string, want ...string) bool { return slices.Equal(got, want) }

// A swarm app finds an open provider without a grant, and does not find a
// private one.
func TestSwarmAppFindsAnOpenProviderWithoutAGrant(t *testing.T) {
	v := newVisibilitySwarm()
	advertise(t, v.a, "player", available)
	advertise(t, v.a, "bitcoin-wallet&open=false", available)

	c := open(t, v.a.OpDiscover, v.app, "services.discover?services=player,bitcoin-wallet", false)
	offered, ng := collect(t, c)
	if !equal(offered, "player") || !equal(ng, "bitcoin-wallet", "player") {
		t.Fatalf("offered %v, not granted %v; want player, and both not granted", offered, ng)
	}
}

// A private provider reaches only a caller holding a permit for it on its
// node, on this node and on a swarm member.
func TestPrivateProviderNeedsAPermit(t *testing.T) {
	for _, where := range []string{"local", "member"} {
		t.Run(where, func(t *testing.T) {
			v := newVisibilitySwarm()
			host := v.a
			if where == "member" {
				host = v.b
			}
			advertise(t, host, "bitcoin-wallet&open=false", available)
			query := "services.discover?services=bitcoin-wallet&reach=swarm"

			offered, _ := collect(t, open(t, v.a.OpDiscover, v.app, query, false))
			if len(offered) != 0 {
				t.Fatalf("offered %v without a permit", offered)
			}

			v.auth.grant(v.app, "bitcoin-wallet", host.node.Identity())
			offered, ng := collect(t, open(t, v.a.OpDiscover, v.app, query, false))
			if !equal(offered, "bitcoin-wallet") {
				t.Fatalf("offered %v with a permit; want the wallet", offered)
			}
			if !equal(ng, "bitcoin-wallet") {
				t.Fatalf("not granted %v; want the wallet, whose other node has no permit", ng)
			}
		})
	}
}

// A discovery without names returns exactly what the caller may see: open
// providers for a swarm app, plus the private ones it holds permits for.
func TestDiscoverWithoutNamesShowsWhatTheCallerMaySee(t *testing.T) {
	v := newVisibilitySwarm()
	advertise(t, v.a, "player", available)
	advertise(t, v.a, "wallet&open=false", available)
	advertise(t, v.a, "secret&open=false", available)
	advertise(t, v.b, "radio", available)
	advertise(t, v.b, "vault&open=false", available)
	v.auth.grant(v.app, "wallet", v.a.node.Identity())
	v.auth.grant(v.app, "vault", v.b.node.Identity())

	offered, ng := collect(t, open(t, v.a.OpDiscover, v.app, "services.discover?reach=swarm", false))
	if !equal(offered, "player", "radio", "vault", "wallet") || ng != nil {
		t.Fatalf("offered %v, not granted %v; want player, radio, vault, wallet and no report", offered, ng)
	}

	friend := astral.GenerateIdentity()
	v.auth.grant(friend, "secret", v.a.node.Identity())
	offered, _ = collect(t, open(t, v.a.OpDiscover, friend, "services.discover?reach=swarm", false))
	if !equal(offered, "secret") {
		t.Fatalf("a caller outside the swarm was offered %v; want only its permitted secret", offered)
	}
}

// A linked identity outside the swarm finds nothing without a permit, open
// providers included.
func TestLinkedIdentityOutsideTheSwarmSeesNothing(t *testing.T) {
	v := newVisibilitySwarm()
	advertise(t, v.a, "player", available)
	friend := astral.GenerateIdentity()

	offered, ng := collect(t, open(t, v.a.OpDiscover, friend, "services.discover?services=player", true))
	if len(offered) != 0 || !equal(ng, "player") {
		t.Fatalf("offered %v, not granted %v; want nothing and player", offered, ng)
	}

	v.auth.grant(friend, "player", v.a.node.Identity())
	if offered, _ := collect(t, open(t, v.a.OpDiscover, friend, "services.discover?services=player", true)); !equal(offered, "player") {
		t.Fatalf("offered %v with a permit; want player", offered)
	}
}

// A private list entry hides an open provider from swarm apps without a
// permit; nothing in the list makes an open:false provider visible.
func TestPrivateListOnlyHides(t *testing.T) {
	v := newVisibilitySwarm()
	_, player := advertise(t, v.a, "player", available)
	advertise(t, v.a, "bitcoin-wallet&open=false", available)
	v.a.coord.SetPrivate(&services.PrivateList{Entries: []*services.PrivateEntry{
		{Service: "player", ProviderID: player},
		{Service: "radio"},
	}})

	offered, _ := collect(t, open(t, v.a.OpDiscover, v.app, "services.discover", false))
	if len(offered) != 0 {
		t.Fatalf("offered %v; want the listed player and the private wallet hidden", offered)
	}

	v.auth.grant(v.app, "player", v.a.node.Identity())
	if offered, _ := collect(t, open(t, v.a.OpDiscover, v.app, "services.discover", false)); !equal(offered, "player") {
		t.Fatalf("offered %v; a permit shows a listed provider", offered)
	}
}

// A follow loses an offering whose provider the list makes private, and sees
// it again when the entry goes.
func TestPrivateListChangeReachesAFollow(t *testing.T) {
	v := newVisibilitySwarm()
	_, player := advertise(t, v.a, "player", available)

	c := open(t, v.a.OpDiscover, v.app, "services.discover?services=player&follow=true", false)
	expect[*services.NotGranted](t, c)
	expect[*services.Update](t, c)
	expect[*astral.EOS](t, c)

	v.a.coord.SetPrivate(&services.PrivateList{Entries: []*services.PrivateEntry{{Service: "player"}}})
	r := expect[*services.Removed](t, c)
	if len(r.Offerings) != 1 || !r.Offerings[0].ProviderID.IsEqual(player) {
		t.Fatalf("removed %v; want the player", r.Offerings)
	}

	v.a.coord.SetPrivate(nil)
	if u := expect[*services.Update](t, c); !u.ProviderID.IsEqual(player) {
		t.Fatalf("got %v; want the player back", u.ProviderID)
	}
}

// A follow without names is refused before any work.
func TestFollowWithoutNamesIsRefused(t *testing.T) {
	v := newVisibilitySwarm()
	c := open(t, v.a.OpDiscover, v.app, "services.discover?follow=true", false)
	expect[*astral.ErrorMessage](t, c)
	expectEnd(t, c)
}

// A member asked with tagged sends each offering with its visibility.
func TestMemberTagsEachOffering(t *testing.T) {
	v := newVisibilitySwarm()
	advertise(t, v.b, "player", available)
	advertise(t, v.b, "bitcoin-wallet&open=false", available)

	q := "services.discover?services=bitcoin-wallet,player&tagged=true&for=" + v.app.String()
	c := open(t, v.b.OpDiscover, v.a.node.Identity(), q, true)
	tags := map[string]bool{}
	for range 2 {
		u := expect[*services.TaggedUpdate](t, c)
		tags[string(u.Update.Name)] = bool(u.Open)
	}
	expect[*astral.EOS](t, c)
	if !tags["player"] || tags["bitcoin-wallet"] {
		t.Fatalf("tags %v; want player open and the wallet private", tags)
	}
}

// A member that predates visibility ignores tagged and sends plain updates;
// the host counts them as open.
func TestUntaggedMemberCountsAsOpen(t *testing.T) {
	v := newVisibilitySwarm()
	advertise(t, v.b, "bitcoin-wallet&open=false", available)
	type oldArgs struct {
		Services string
		Follow   bool
		Reach    string
		For      string
		In, Out  string
	}
	v.nodeA.op = func(ctx *astral.Context, q *routing.IncomingQuery, a oldArgs) error {
		return v.b.OpDiscover(ctx, q, opDiscoverArgs{Services: a.Services, Follow: a.Follow, Reach: a.Reach, For: a.For, In: a.In, Out: a.Out})
	}

	done := make(chan struct{})
	go func() {
		defer close(done)
		offered, _ := collect(t, open(t, v.a.OpDiscover, v.app, "services.discover?services=bitcoin-wallet&reach=swarm", false))
		if !equal(offered, "bitcoin-wallet") {
			t.Errorf("offered %v; want the old member's wallet, counted as open", offered)
		}
	}()
	select {
	case <-done:
	case <-time.After(2 * testWait):
		t.Fatal("no outcome")
	}
}

// A member's tagged follow re-sends a shown offering with its new tag when
// the member's private list changes, and the host then removes it from a
// caller that may no longer see it.
func TestPrivateListChangeOnAMemberRetagsItsFollow(t *testing.T) {
	v := newVisibilitySwarm()
	advertise(t, v.b, "player", available)

	q := "services.discover?services=player&follow=true&tagged=true&for=" + v.app.String()
	member := open(t, v.b.OpDiscover, v.a.node.Identity(), q, true)
	if u := expect[*services.TaggedUpdate](t, member); !bool(u.Open) {
		t.Fatal("the open player is tagged private")
	}
	expect[*astral.EOS](t, member)

	host := open(t, v.a.OpDiscover, v.app, "services.discover?services=player&follow=true&reach=swarm", false)
	expect[*services.NotGranted](t, host)
	expect[*services.Update](t, host)
	expect[*astral.EOS](t, host)

	v.b.coord.SetPrivate(&services.PrivateList{Entries: []*services.PrivateEntry{{Service: "player"}}})
	// note: the host's carry asked the same provider for the same caller, so
	// the member's follow can first receive that answer, tagged before the change.
	for i := 0; ; i++ {
		if u := expect[*services.TaggedUpdate](t, member); !bool(u.Open) {
			break
		}
		if i == 2 {
			t.Fatal("the listed player stays tagged open")
		}
	}
	if r := expect[*services.Removed](t, host); len(r.Offerings) != 1 {
		t.Fatalf("removed %v; want the player", r.Offerings)
	}
}
