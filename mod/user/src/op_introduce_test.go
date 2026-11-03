package user

import (
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/astralp2p/astral-go/api/auth"
	"github.com/astralp2p/astral-go/api/dir"
	"github.com/astralp2p/astral-go/api/user"
	"github.com/astralp2p/astral-go/astral"
	"github.com/astralp2p/astral-go/astral/log"
	"github.com/astralp2p/astral-go/lib/query"
	"github.com/astralp2p/astral-go/lib/routing"
	authmod "github.com/astralp2p/astrald/mod/auth"
	objectsmod "github.com/astralp2p/astrald/mod/objects"
	"github.com/astralp2p/astrald/mod/scheduler"
)

// swarmAdmins is an auth module that grants user.AdminSwarmAction to the listed
// actors alone and records every action it is asked about.
type swarmAdmins struct {
	authmod.Module
	admins []*astral.Identity

	mu    sync.Mutex
	asked []auth.ActionObject
}

func (a *swarmAdmins) Authorize(_ *astral.Context, action auth.ActionObject) bool {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.asked = append(a.asked, action)

	swarm, ok := action.(*user.AdminSwarmAction)
	return ok && slicesContainsIdentity(a.admins, swarm.Actor())
}

func (a *swarmAdmins) recorded() []auth.ActionObject {
	a.mu.Lock()
	defer a.mu.Unlock()
	return append([]auth.ActionObject(nil), a.asked...)
}

// introducePush is one push the op made.
type introducePush struct {
	target   *astral.Identity
	object   astral.Object
	zone     astral.Zone
	deadline time.Time
}

// introducePusher is an objects module that records every push and answers err.
type introducePusher struct {
	objectsmod.Module
	err    error
	pushed chan introducePush
}

func (p *introducePusher) Push(ctx *astral.Context, target *astral.Identity, obj astral.Object) error {
	deadline, _ := ctx.Deadline()
	p.pushed <- introducePush{target: target, object: obj, zone: ctx.Zone(), deadline: deadline}
	return p.err
}

// introduceFixture is node C, claimed by user U, that names node B "peer", U
// "user", itself "self" and the zero identity "anyone". U alone holds
// user.AdminSwarmAction; adminID is another identity a case can grant it to.
type introduceFixture struct {
	mod       *Module
	userID    *astral.Identity
	nodeID    *astral.Identity
	peerID    *astral.Identity
	adminID   *astral.Identity
	auth      *swarmAdmins
	objects   *introducePusher
	scheduler *recordingScheduler
}

func newIntroduceFixture(t *testing.T) *introduceFixture {
	t.Helper()

	f := &introduceFixture{
		userID:  astral.GenerateIdentity(),
		nodeID:  astral.GenerateIdentity(),
		peerID:  astral.GenerateIdentity(),
		adminID: astral.GenerateIdentity(),
	}
	f.auth = &swarmAdmins{admins: []*astral.Identity{f.userID}}
	f.objects = &introducePusher{pushed: make(chan introducePush, 1)}
	f.scheduler = &recordingScheduler{tasks: make(chan scheduler.Task, 1)}

	f.mod = &Module{
		Deps: Deps{
			Auth:      f.auth,
			Objects:   f.objects,
			Scheduler: f.scheduler,
			Dir: &namedDir{names: map[string]*astral.Identity{
				"peer":   f.peerID,
				"user":   f.userID,
				"self":   f.nodeID,
				"anyone": astral.Anyone,
			}},
		},
		node: &identityNode{id: f.nodeID},
		log:  log.New(f.nodeID),
		db:   testDB(t),
	}
	f.setActiveContract(t, nodeContract(f.userID, f.nodeID))

	return f
}

func (f *introduceFixture) setActiveContract(t *testing.T, signed *auth.SignedContract) {
	t.Helper()
	if err := f.mod.config.ActiveContract.Set(nil, signed); err != nil {
		t.Fatalf("seed active contract: %v", err)
	}
}

func (f *introduceFixture) expel(t *testing.T, subject *astral.Identity) {
	t.Helper()
	if err := f.mod.db.StoreExpulsion(sampleSigned(f.userID, subject)); err != nil {
		t.Fatalf("store expulsion: %v", err)
	}
}

// introduceQuery is one user.introduce query: its caller, its origin, and the
// name of the node it introduces this node to.
type introduceQuery struct {
	caller *astral.Identity
	origin string
	node   string
}

// routeIntroduce sends iq in a context without the Network zone, as a session
// without a token has. It waits for the op to return and answers what the op sent.
func routeIntroduce(t *testing.T, f *introduceFixture, iq introduceQuery) []astral.Object {
	t.Helper()

	op, err := routing.NewOp(f.mod.OpIntroduce)
	if err != nil {
		t.Fatalf("new op: %v", err)
	}
	reports := make(chan *routing.Report, 1)
	op.LogFunc = func(r *routing.Report) { reports <- r }

	ctx, cancel := astral.NewContext(nil).ExcludeZone(astral.ZoneNetwork).WithTimeout(10 * time.Second)
	defer cancel()

	q := astral.Launch(query.New(iq.caller, f.nodeID, user.OpIntroduce+"?identity="+iq.node, nil))
	if iq.origin != "" {
		q.Extra.Set("origin", iq.origin)
	}

	w := &answerWriter{}
	requester, err := op.RouteQuery(ctx, q, w)
	if err != nil {
		t.Fatalf("route: %v", err)
	}
	defer requester.Close()

	select {
	case <-reports:
	case <-time.After(10 * time.Second):
		t.Fatal("the op never returned")
	}
	return w.objects(t)
}

// requirePushed checks the one push the op made: the active contract to the
// named node, as this node's own query with the Network zone and the 30 s bound.
func requirePushed(t *testing.T, f *introduceFixture) {
	t.Helper()

	// note: the op has returned, so a push it made is already recorded.
	var push introducePush
	select {
	case push = <-f.objects.pushed:
	default:
		t.Fatal("nothing was pushed")
	}
	if !push.target.IsEqual(f.peerID) {
		t.Fatalf("pushed to %v; want the named node %v", push.target, f.peerID)
	}
	if push.object != f.mod.ActiveContract() {
		t.Fatalf("pushed %v; want the active contract", push.object)
	}
	if !push.zone.Is(astral.ZoneNetwork) {
		t.Fatalf("pushed in zone %v; want the Network zone", push.zone)
	}
	if left := time.Until(push.deadline); left <= introducePushTimeout-5*time.Second || left > introducePushTimeout {
		t.Fatalf("push deadline in %v; want %v", left, introducePushTimeout)
	}
}

// requireSyncScheduled checks that one sync with the named node was scheduled.
func requireSyncScheduled(t *testing.T, f *introduceFixture) {
	t.Helper()

	select {
	case task := <-f.scheduler.tasks:
		sync, ok := task.(*SyncNodesTask)
		if !ok || !sync.remoteIdentity.IsEqual(f.peerID) {
			t.Fatalf("scheduled %v; want a sync with the named node", task)
		}
	default:
		t.Fatal("no sync was scheduled")
	}
}

// requireAskedAdminSwarm checks the one authorization the op asked for:
// AdminSwarm for caller over the named node.
func requireAskedAdminSwarm(t *testing.T, f *introduceFixture, caller *astral.Identity) {
	t.Helper()

	asked := f.auth.recorded()
	if len(asked) != 1 {
		t.Fatalf("asked %d authorizations; want one", len(asked))
	}
	swarm, ok := asked[0].(*user.AdminSwarmAction)
	if !ok || !swarm.Actor().IsEqual(caller) || !swarm.Subject.IsEqual(f.peerID) {
		t.Fatalf("asked %v; want AdminSwarm for the caller over the named node", asked[0])
	}
}

// TestIntroducePushesTheContractAndSyncs: user.introduce from a caller holding
// AdminSwarm pushes the active contract to the named node, schedules a sync with
// it, and answers an ack. Every origin but network passes.
func TestIntroducePushesTheContractAndSyncs(t *testing.T) {
	cases := []struct {
		name   string
		origin string
		admin  bool
	}{
		{"the user, unset origin", "", false},
		{"the user, local origin", astral.OriginLocal, false},
		{"the user, mcp origin", astral.OriginMCP, false},
		{"another identity holding AdminSwarm", "", true},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			f := newIntroduceFixture(t)
			caller := f.userID
			if c.admin {
				f.auth.admins = append(f.auth.admins, f.adminID)
				caller = f.adminID
			}

			objs := routeIntroduce(t, f, introduceQuery{caller: caller, origin: c.origin, node: "peer"})
			if len(objs) != 1 {
				t.Fatalf("the op answered %v; want one ack", objs)
			}
			if _, ok := objs[0].(*astral.Ack); !ok {
				t.Fatalf("the op answered %v; want an ack", objs[0])
			}

			requirePushed(t, f)
			requireSyncScheduled(t, f)
			requireAskedAdminSwarm(t, f, caller)
		})
	}
}

// introduceRefusal is one query user.introduce refuses, and its answer. A nil
// caller is the user; setup prepares the fixture.
type introduceRefusal struct {
	name   string
	origin string
	caller func(f *introduceFixture) *astral.Identity
	node   string
	setup  func(t *testing.T, f *introduceFixture)
	answer astral.Error
}

var introduceRefusals = []introduceRefusal{
	{"network origin", astral.OriginNetwork, nil, "peer", nil, astral.NewErrOriginNotAllowed(astral.OriginNetwork)},
	{"no active contract", "", nil, "peer", func(t *testing.T, f *introduceFixture) {
		f.setActiveContract(t, nil)
	}, user.ErrNoActiveContract},
	{"unknown node", "", nil, "nobody", nil, dir.ErrUnknownIdentity},
	{"anonymous node", "", nil, "anyone", nil, astral.ErrZeroIdentity},
	{"this node", "", nil, "self", nil, user.ErrIntroduceSelf},
	{"the user", "", nil, "user", nil, user.ErrIntroduceSelf},
	{"caller without AdminSwarm", "", func(*introduceFixture) *astral.Identity { return astral.GenerateIdentity() }, "peer", nil, auth.ErrNotPermitted},
	{"this node as caller", "", func(f *introduceFixture) *astral.Identity { return f.nodeID }, "peer", nil, auth.ErrNotPermitted},
	{"expelled caller with AdminSwarm", "", func(f *introduceFixture) *astral.Identity { return f.adminID }, "peer", func(t *testing.T, f *introduceFixture) {
		f.auth.admins = append(f.auth.admins, f.adminID)
		f.expel(t, f.adminID)
	}, auth.ErrNotPermitted},
	{"expired active contract", "", nil, "peer", func(t *testing.T, f *introduceFixture) {
		signed := nodeContract(f.userID, f.nodeID)
		signed.ExpiresAt = astral.Time(time.Now().Add(-time.Minute))
		f.setActiveContract(t, signed)
	}, astral.Err(auth.ErrContractExpired)},
	{"expelled node", "", nil, "peer", func(t *testing.T, f *introduceFixture) {
		f.expel(t, f.peerID)
	}, user.ErrExpelled},
}

// TestIntroduceRejects: every refused query is answered with a single
// error_message, and the op pushes nothing and schedules nothing.
func TestIntroduceRejects(t *testing.T) {
	for _, c := range introduceRefusals {
		t.Run(c.name, func(t *testing.T) {
			f := newIntroduceFixture(t)
			if c.setup != nil {
				c.setup(t, f)
			}
			iq := introduceQuery{caller: f.userID, origin: c.origin, node: c.node}
			if c.caller != nil {
				iq.caller = c.caller(f)
			}

			objs := routeIntroduce(t, f, iq)
			if len(objs) != 1 || objs[0].ObjectType() != "error_message" || objs[0].(astral.Error).Error() != c.answer.Error() {
				t.Fatalf("the op answered %v; want %q", objs, c.answer)
			}

			select {
			case push := <-f.objects.pushed:
				t.Fatalf("pushed to %v; a refused query pushes nothing", push.target)
			case task := <-f.scheduler.tasks:
				t.Fatalf("scheduled %v; a refused query schedules nothing", task)
			default:
			}
		})
	}
}

// TestIntroduceAnswersAFailedPush: a push the named node refuses, or that does
// not complete, is answered with an error naming the node, and no sync is
// scheduled.
func TestIntroduceAnswersAFailedPush(t *testing.T) {
	f := newIntroduceFixture(t)
	f.objects.err = errors.New("rejected")

	objs := routeIntroduce(t, f, introduceQuery{caller: f.userID, node: "peer"})

	want := "push to " + f.peerID.String() + ": rejected"
	if len(objs) != 1 || objs[0].ObjectType() != "error_message" || objs[0].(astral.Error).Error() != want {
		t.Fatalf("the op answered %v; want %q", objs, want)
	}
	select {
	case push := <-f.objects.pushed:
		if !push.target.IsEqual(f.peerID) {
			t.Fatalf("pushed to %v; want the named node", push.target)
		}
	default:
		t.Fatal("nothing was pushed")
	}
	select {
	case task := <-f.scheduler.tasks:
		t.Fatalf("scheduled %v; a failed push schedules nothing", task)
	default:
	}
}
