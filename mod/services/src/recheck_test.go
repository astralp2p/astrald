package services

import (
	"testing"
	"time"

	"github.com/astralp2p/astral-go/api/auth"
	"github.com/astralp2p/astral-go/api/events"
	"github.com/astralp2p/astral-go/api/services"
	userapi "github.com/astralp2p/astral-go/api/user"
	"github.com/astralp2p/astral-go/astral"
)

// Authority changes reach open follows, carries and bindings: the fakes run
// the change callbacks as apphost and auth do, and both nodes re-check as Run
// sets them up.
//
// Spec: .ai/system/protocols/services/ops/services.discover.md#authority-changes

// quietWait is how long a test watches for an object that must not arrive.
const quietWait = 300 * time.Millisecond

func newRecheckSwarm(t *testing.T) *visibilitySwarm {
	t.Helper()
	v := newVisibilitySwarm()
	ctx, cancel := astral.NewContext(nil).WithCancel()
	t.Cleanup(cancel)
	for _, mod := range []*Module{v.a, v.b} {
		mod.watchAuthority()
		go mod.runRechecks(ctx)
	}
	return v
}

// expectQuiet fails when p receives an object within quietWait. It is the last
// read of a test.
func expectQuiet(t *testing.T, p *peer) {
	t.Helper()
	got := make(chan astral.Object, 1)
	go func() {
		if obj, err := p.ch.Receive(); err == nil {
			got <- obj
		}
	}()
	select {
	case obj := <-got:
		t.Fatalf("received %s %v; want nothing", obj.ObjectType(), obj)
	case <-time.After(quietWait):
	}
}

func expectRemoved(t *testing.T, p *peer, provider *astral.Identity, cause services.RemovalCause) {
	t.Helper()
	r := expect[*services.Removed](t, p)
	if len(r.Offerings) != 1 || !r.Offerings[0].ProviderID.IsEqual(provider) {
		t.Fatalf("removed %+v; want %v only", r.Offerings, provider)
	}
	if got := services.RemovalCause(r.Cause); got != cause {
		t.Fatalf("removal cause %v; want %v", got, cause)
	}
}

func expectNotGranted(t *testing.T, p *peer, want ...string) {
	t.Helper()
	var got []string
	for _, s := range expect[*services.NotGranted](t, p).Services {
		got = append(got, string(s))
	}
	if !equal(got, want...) {
		t.Fatalf("not granted %v; want %v", got, want)
	}
}

func expectUpdateFrom(t *testing.T, p *peer, provider *astral.Identity) {
	t.Helper()
	if u := expect[*services.Update](t, p); !u.ProviderID.IsEqual(provider) {
		t.Fatalf("update from %v; want %v", u.ProviderID, provider)
	}
}

// askedProvider answers every ask available and signals each ask.
func askedProvider(asked chan<- struct{}) func(*services.Ask) *services.Update {
	return func(*services.Ask) *services.Update {
		select {
		case asked <- struct{}{}:
		default:
		}
		return &services.Update{Available: true}
	}
}

func waitAsked(t *testing.T, asked <-chan struct{}) {
	t.Helper()
	select {
	case <-asked:
	case <-time.After(testWait):
		t.Fatal("the provider was not asked")
	}
}

// A revoke mid-follow removes the private local offering with cause hidden,
// sends the new not_granted list, and filters a private provider registered
// later.
func TestRevokeMidFollowRetractsThePrivateOffering(t *testing.T) {
	v := newRecheckSwarm(t)
	nodeA := v.a.node.Identity()
	_, private := advertise(t, v.a, "contacts&open=false", available)
	v.auth.grant(v.app, "contacts", nodeA)

	c := open(t, v.a.OpDiscover, v.app, "services.discover?services=contacts&follow=true", false)
	expectUpdateFrom(t, c, private)
	expect[*astral.EOS](t, c)

	v.auth.revoke(v.app, "contacts", nodeA)
	v.apps.grantsChanged(v.app)
	expectRemoved(t, c, private, services.CauseHidden)
	expectNotGranted(t, c, "contacts")

	asked := make(chan struct{}, 1)
	advertise(t, v.a, "contacts&open=false", askedProvider(asked))
	waitAsked(t, asked)
	_, openP := advertise(t, v.a, "contacts", available)
	expectUpdateFrom(t, c, openP)
	expectQuiet(t, c)
}

// A member's private offering shown through a permit is removed after the
// revoke, and a carry restored after a lost stream does not show it again.
func TestRevokeRetractsAMemberOfferingAndARestartKeepsItHidden(t *testing.T) {
	v := newRecheckSwarm(t)
	node := newCuttableNode(v.nodeA)
	v.a.node = node
	nodeB := v.b.node.Identity()
	_, private := advertise(t, v.b, "contacts&open=false", available)
	v.auth.grant(v.app, "contacts", nodeB)

	c := open(t, v.a.OpDiscover, v.app, "services.discover?services=contacts&reach=swarm&follow=true", false)
	offered, ng := collect(t, c)
	if !equal(offered, "contacts") || !equal(ng, "contacts") {
		t.Fatalf("offered %v, not granted %v; want the member's private offering, not granted on A", offered, ng)
	}
	node.waitEOS(t)

	v.auth.revoke(v.app, "contacts", nodeB)
	v.apps.grantsChanged(v.app)
	expectRemoved(t, c, private, services.CauseHidden)

	node.cut()
	node.waitEOS(t) // the restored carry reached the member's initial outcome
	_, openP := advertise(t, v.b, "contacts", available)
	expectUpdateFrom(t, c, openP)
	expectQuiet(t, c)
}

// The change callback only schedules: a re-check blocked in authorization
// delays no later grant change.
func TestBlockedRecheckDoesNotDelayGrantChanges(t *testing.T) {
	v := newRecheckSwarm(t)
	nodeA := v.a.node.Identity()
	_, private := advertise(t, v.a, "contacts&open=false", available)
	v.auth.grant(v.app, "contacts", nodeA)
	c := open(t, v.a.OpDiscover, v.app, "services.discover?services=contacts&follow=true", false)
	expectUpdateFrom(t, c, private)
	expect[*astral.EOS](t, c)

	block := make(chan struct{})
	v.auth.setBlock(block)
	v.auth.revoke(v.app, "contacts", nodeA)
	v.apps.grantsChanged(v.app)
	time.Sleep(100 * time.Millisecond) // the re-check now waits in Authorize

	returned := make(chan struct{})
	go func() {
		for range 3 {
			v.apps.grantsChanged(v.app)
		}
		close(returned)
	}()
	select {
	case <-returned:
	case <-time.After(time.Second):
		t.Fatal("a grant change waited for the blocked re-check")
	}

	v.auth.setBlock(nil)
	close(block)
	expectRemoved(t, c, private, services.CauseHidden)
}

// A grant mid-follow sends the refreshed not_granted list, then the private
// local provider, without a reopen.
func TestGrantMidFollowRevealsThePrivateProvider(t *testing.T) {
	v := newRecheckSwarm(t)
	nodeA := v.a.node.Identity()
	_, private := advertise(t, v.a, "contacts&open=false", available)

	c := open(t, v.a.OpDiscover, v.app, "services.discover?services=contacts&follow=true", false)
	offered, ng := collect(t, c)
	if len(offered) != 0 || !equal(ng, "contacts") {
		t.Fatalf("offered %v, not granted %v; want nothing, contacts not granted", offered, ng)
	}

	v.auth.grant(v.app, "contacts", nodeA)
	v.apps.grantsChanged(v.app)
	expectNotGranted(t, c)
	expectUpdateFrom(t, c, private)
}

// A grant on a member mid-follow delivers the member's private offering that
// was filtered before, through a restarted carry.
func TestGrantMidFollowDeliversAMemberPrivateOffering(t *testing.T) {
	v := newRecheckSwarm(t)
	nodeB := v.b.node.Identity()
	_, private := advertise(t, v.b, "contacts&open=false", available)

	c := open(t, v.a.OpDiscover, v.app, "services.discover?services=contacts&reach=swarm&follow=true", false)
	offered, ng := collect(t, c)
	if len(offered) != 0 || !equal(ng, "contacts") {
		t.Fatalf("offered %v, not granted %v; want nothing, contacts not granted", offered, ng)
	}

	v.auth.grant(v.app, "contacts", nodeB)
	v.apps.grantsChanged(v.app)
	// note: not granted on A still, so the list does not change.
	expectUpdateFrom(t, c, private)
}

// A follow opened on an unclaimed node, where no app is a swarm caller, gets
// open local providers once the claim emits swarm_member_joined.
func TestClaimRevealsOpenProvidersToALocalFollow(t *testing.T) {
	v := newRecheckSwarm(t)
	nodeA := v.a.node.Identity()
	unclaimed := &swarmUser{}
	v.a.User = unclaimed
	v.apps.setApps()
	_, player := advertise(t, v.a, "player", available)

	c := open(t, v.a.OpDiscover, v.app, "services.discover?services=player&follow=true", false)
	offered, ng := collect(t, c)
	if len(offered) != 0 || !equal(ng, "player") {
		t.Fatalf("offered %v, not granted %v; want nothing on an unclaimed node", offered, ng)
	}

	unclaimed.set([]*astral.Identity{nodeA, v.b.node.Identity()}, astral.GenerateIdentity())
	v.apps.setApps(v.app)
	joined := &events.Event{Data: &userapi.SwarmMemberJoined{NodeID: v.b.node.Identity()}}
	if err := v.a.ReceiveObject(&linkDrop{sender: nodeA, obj: joined}); err != nil {
		t.Fatal(err)
	}
	expectUpdateFrom(t, c, player)
}

// A relay-for contract the caller issued and indexed after the follow opened
// makes it a swarm app; the follow then gets open local providers.
func TestIndexedRelayContractRevealsOpenProviders(t *testing.T) {
	v := newRecheckSwarm(t)
	nodeA := v.a.node.Identity()
	v.apps.setApps()
	_, player := advertise(t, v.a, "player", available)

	c := open(t, v.a.OpDiscover, v.app, "services.discover?services=player&follow=true", false)
	if offered, _ := collect(t, c); len(offered) != 0 {
		t.Fatalf("offered %v before the contract; want nothing", offered)
	}

	v.apps.setApps(v.app)
	v.auth.indexed(&auth.SignedContract{Contract: &auth.Contract{Issuer: v.app, Subject: nodeA}})
	expectUpdateFrom(t, c, player)
}

// A withdrawn contract the caller relied on for its permit retracts the
// private offering.
func TestWithdrawnContractRetractsThePrivateOffering(t *testing.T) {
	v := newRecheckSwarm(t)
	nodeA := v.a.node.Identity()
	_, private := advertise(t, v.a, "contacts&open=false", available)
	v.auth.grant(v.app, "contacts", nodeA)
	c := open(t, v.a.OpDiscover, v.app, "services.discover?services=contacts&follow=true", false)
	expectUpdateFrom(t, c, private)
	expect[*astral.EOS](t, c)

	v.auth.revoke(v.app, "contacts", nodeA)
	v.auth.withdrawn(astral.GenerateIdentity())
	expectRemoved(t, c, private, services.CauseHidden)
	expectNotGranted(t, c, "contacts")
}

// A provider whose serve_apps was revoked loses its binding: followers get
// services.removed with cause binding_closed and the advertise channel ends.
func TestServeAppsRevokeClosesTheBinding(t *testing.T) {
	v := newRecheckSwarm(t)
	provider := astral.GenerateIdentity()
	pc := open(t, v.a.OpAdvertise, provider, "services.advertise?services=player", false)
	expect[*astral.Ack](t, pc)

	c := open(t, v.a.OpDiscover, v.app, "services.discover?services=player&follow=true", false)
	ask := expect[*services.Ask](t, pc)
	if err := pc.ch.Send(&services.Answer{RequestID: ask.RequestID, Update: &services.Update{Available: true, Name: ask.Service}}); err != nil {
		t.Fatal(err)
	}
	expectUpdateFrom(t, c, provider)
	expect[*astral.EOS](t, c)

	v.auth.refuse(auth.ServeAppsAction{}.ObjectType())
	v.apps.grantsChanged(provider)
	expectRemoved(t, c, provider, services.CauseBindingClosed)
	expectEnd(t, pc)
}

// A member that leaves the swarm has the carry it serves here closed when
// swarm_member_left arrives, while the link stays up; the carrying node's app
// sees the member's offering removed within a second.
func TestMemberLeftClosesTheCarryItServes(t *testing.T) {
	v := newRecheckSwarm(t)
	nodeA, nodeB := v.a.node.Identity(), v.b.node.Identity()
	userB := &swarmUser{swarm: []*astral.Identity{nodeA, nodeB}}
	v.b.User = userB
	_, player := advertise(t, v.b, "player", available)

	c := open(t, v.a.OpDiscover, v.app, "services.discover?services=player&reach=swarm&follow=true", false)
	offered, _ := collect(t, c)
	if !equal(offered, "player") {
		t.Fatalf("offered %v; want the member's player", offered)
	}

	userB.set([]*astral.Identity{nodeB}, nil)
	left := &events.Event{Data: &userapi.SwarmMemberLeft{NodeID: nodeA}}
	start := time.Now()
	if err := v.b.ReceiveObject(&linkDrop{sender: nodeB, obj: left}); err != nil {
		t.Fatal(err)
	}
	expectRemoved(t, c, player, services.CauseMemberLost)
	if d := time.Since(start); d > time.Second {
		t.Fatalf("the carry closed %v after swarm_member_left; want within 1 s", d)
	}
}
