package user

import (
	"errors"
	"net"
	"sync"
	"testing"
	"time"

	"github.com/astralp2p/astral-go/api/auth"
	"github.com/astralp2p/astral-go/api/events"
	"github.com/astralp2p/astral-go/api/ip"
	"github.com/astralp2p/astral-go/api/nearby"
	"github.com/astralp2p/astral-go/astral"
	"github.com/astralp2p/astral-go/astral/log"
	objectsmod "github.com/astralp2p/astrald/mod/objects"
	"github.com/astralp2p/astrald/mod/scheduler"
)

// contactCall is one step of a nearby contact: a contract read or a push.
type contactCall struct {
	op     string // "contract" or "push"
	peer   *astral.Identity
	object astral.Object
	zone   astral.Zone
}

// contactLog records the steps of nearby contacts in order, and reports each
// contract read on reads.
type contactLog struct {
	mu    sync.Mutex
	calls []contactCall
	reads chan *astral.Identity
}

func (l *contactLog) add(c contactCall) {
	l.mu.Lock()
	l.calls = append(l.calls, c)
	l.mu.Unlock()

	if c.op == "contract" {
		l.reads <- c.peer
	}
}

func (l *contactLog) list() []contactCall {
	l.mu.Lock()
	defer l.mu.Unlock()
	return append([]contactCall(nil), l.calls...)
}

// contactPusher is an objects module that records every push.
type contactPusher struct {
	objectsmod.Module
	log *contactLog
}

func (p *contactPusher) Push(ctx *astral.Context, target *astral.Identity, obj astral.Object) error {
	p.log.add(contactCall{op: "push", peer: target, object: obj, zone: ctx.Zone()})
	return nil
}

// contactFixture is node nodeID of user userID, holding its active contract,
// whose fakes record every step of a nearby contact. peerID is a node of the
// user that is no swarm member here yet. answer is what peerID answers
// user.contract with, by default a valid node contract.
type contactFixture struct {
	mod       *Module
	nodeID    *astral.Identity
	userID    *astral.Identity
	peerID    *astral.Identity
	auth      *indexAuth
	nodes     *linkNodes
	scheduler *recordingScheduler
	calls     *contactLog
	now       time.Time
	answer    func(f *contactFixture) (*auth.SignedContract, error)
}

func newContactFixture(t *testing.T) *contactFixture {
	t.Helper()

	f := &contactFixture{
		nodeID:    astral.GenerateIdentity(),
		userID:    astral.GenerateIdentity(),
		peerID:    astral.GenerateIdentity(),
		auth:      &indexAuth{},
		nodes:     newLinkNodes(),
		scheduler: &recordingScheduler{tasks: make(chan scheduler.Task, 8)},
		calls:     &contactLog{reads: make(chan *astral.Identity, 16)},
		now:       time.Now(),
	}
	f.answer = func(f *contactFixture) (*auth.SignedContract, error) {
		return nodeContract(f.userID, f.peerID), nil
	}

	ctx, cancel := astral.NewContext(nil).WithCancel()
	t.Cleanup(cancel)

	f.mod = &Module{
		Deps:             Deps{Auth: f.auth, Nodes: f.nodes, Objects: &contactPusher{log: f.calls}, Scheduler: f.scheduler},
		ctx:              ctx,
		node:             &identityNode{id: f.nodeID},
		log:              log.New(f.nodeID),
		db:               singleConnDB(t),
		dials:            newDialMap(),
		now:              func() time.Time { return f.now },
		readPeerContract: f.readContract,
	}
	if err := f.mod.config.ActiveContract.Set(nil, nodeContract(f.userID, f.nodeID)); err != nil {
		t.Fatalf("seed active contract: %v", err)
	}
	return f
}

// singleConnDB is testDB on one connection, so goroutines share its :memory: database.
func singleConnDB(t *testing.T) *DB {
	t.Helper()

	db := testDB(t)
	sqlDB, err := db.DB.DB()
	if err != nil {
		t.Fatalf("sql db: %v", err)
	}
	sqlDB.SetMaxOpenConns(1)
	return db
}

func (f *contactFixture) readContract(ctx *astral.Context, peer *astral.Identity) (*auth.SignedContract, error) {
	f.calls.add(contactCall{op: "contract", peer: peer, zone: ctx.Zone()})
	return f.answer(f)
}

// seen is the PeerSeen of node of user.
func (f *contactFixture) seen(node, user *astral.Identity) *nearby.PeerSeen {
	return &nearby.PeerSeen{NodeID: node, UserID: user, Via: nearby.ViaHint, SourceIP: ip.IP(net.IPv4(192, 168, 1, 7))}
}

// nextAttempt returns the dial map's next allowed attempt at node.
func (f *contactFixture) nextAttempt(node *astral.Identity) (time.Time, bool) {
	f.mod.dials.mu.Lock()
	defer f.mod.dials.mu.Unlock()
	next, ok := f.mod.dials.next[node.String()]
	return next, ok
}

// addMember indexes a node contract from the user for node, making node a swarm member.
func (f *contactFixture) addMember(node *astral.Identity) {
	f.auth.seed(nodeContract(f.userID, node))
}

// requireNoContact fails when anything was read, pushed, scheduled or held in the dial map.
func (f *contactFixture) requireNoContact(t *testing.T, node *astral.Identity) {
	t.Helper()

	if calls := f.calls.list(); len(calls) != 0 {
		t.Fatalf("contact steps %v; want none", calls)
	}
	select {
	case task := <-f.scheduler.tasks:
		t.Fatalf("scheduled %v; want nothing", task)
	default:
	}
	if next, ok := f.nextAttempt(node); ok {
		t.Fatalf("the dial map holds %v until %v; want no entry", node, next)
	}
}

// requireRefused fails unless the contract of peer was read, nothing was pushed
// or scheduled, and the next attempt waits dialIntervalAfterRefusal.
func (f *contactFixture) requireRefused(t *testing.T) {
	t.Helper()

	calls := f.calls.list()
	if len(calls) != 1 || calls[0].op != "contract" || !calls[0].peer.IsEqual(f.peerID) {
		t.Fatalf("contact steps %v; want one contract read of the peer", calls)
	}
	select {
	case task := <-f.scheduler.tasks:
		t.Fatalf("scheduled %v; want nothing", task)
	default:
	}
	if next, _ := f.nextAttempt(f.peerID); !next.Equal(f.now.Add(dialIntervalAfterRefusal)) {
		t.Fatalf("next attempt at %v; want %v", next, f.now.Add(dialIntervalAfterRefusal))
	}
}

// TestPeerSeenIgnores: a node acts on no PeerSeen of a node that is not of its
// user, of itself, of an expelled node or of a linked sibling, nor on any
// PeerSeen while it has no user.
func TestPeerSeenIgnores(t *testing.T) {
	cases := []struct {
		name  string
		event func(t *testing.T, f *contactFixture) *nearby.PeerSeen
	}{
		{"no user here", func(t *testing.T, f *contactFixture) *nearby.PeerSeen {
			if err := f.mod.config.ActiveContract.Clear(nil); err != nil {
				t.Fatalf("clear: %v", err)
			}
			return f.seen(f.peerID, f.userID)
		}},
		{"another user", func(_ *testing.T, f *contactFixture) *nearby.PeerSeen {
			return f.seen(f.peerID, astral.GenerateIdentity())
		}},
		{"no user", func(_ *testing.T, f *contactFixture) *nearby.PeerSeen { return f.seen(f.peerID, nil) }},
		{"zero user", func(_ *testing.T, f *contactFixture) *nearby.PeerSeen { return f.seen(f.peerID, &astral.Identity{}) }},
		{"this node", func(_ *testing.T, f *contactFixture) *nearby.PeerSeen { return f.seen(f.nodeID, f.userID) }},
		{"zero node", func(_ *testing.T, f *contactFixture) *nearby.PeerSeen { return f.seen(&astral.Identity{}, f.userID) }},
		{"expelled node", func(t *testing.T, f *contactFixture) *nearby.PeerSeen {
			if err := f.mod.db.StoreExpulsion(sampleSigned(f.userID, f.peerID)); err != nil {
				t.Fatalf("store expulsion: %v", err)
			}
			return f.seen(f.peerID, f.userID)
		}},
		{"linked sibling", func(t *testing.T, f *contactFixture) *nearby.PeerSeen {
			f.addMember(f.peerID)
			f.nodes.setLinked(f.peerID, true)
			f.mod.addSibling(f.peerID, func() {}, func() { t.Error("woke the link task of a linked sibling") })
			return f.seen(f.peerID, f.userID)
		}},
		{"no event", func(*testing.T, *contactFixture) *nearby.PeerSeen { return nil }},
	}
	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			f := newContactFixture(t)
			f.mod.onPeerSeen(tt.event(t, f))
			f.requireNoContact(t, f.peerID)
		})
	}
}

// TestPeerSeenWakesAnUnlinkedSibling: a swarm member seen nearby that is not
// linked wakes its link task; nothing is read or pushed.
func TestPeerSeenWakesAnUnlinkedSibling(t *testing.T) {
	f := newContactFixture(t)
	f.addMember(f.peerID)
	woken := 0
	f.mod.addSibling(f.peerID, func() {}, func() { woken++ })

	f.mod.onPeerSeen(f.seen(f.peerID, f.userID))

	if woken != 1 {
		t.Fatalf("woke the link task %v times; want once", woken)
	}
	f.requireNoContact(t, f.peerID)
}

// TestPeerSeenStartsALinkTask: a swarm member seen nearby without a link task
// gets one.
func TestPeerSeenStartsALinkTask(t *testing.T) {
	f := newContactFixture(t)
	f.addMember(f.peerID)

	f.mod.onPeerSeen(f.seen(f.peerID, f.userID))

	select {
	case task := <-f.scheduler.tasks:
		link, ok := task.(*MaintainLinkTask)
		if !ok || !link.Target.IsEqual(f.peerID) {
			t.Fatalf("scheduled %v; want a link task of the peer", task)
		}
	default:
		t.Fatal("scheduled no link task")
	}
	if calls := f.calls.list(); len(calls) != 0 {
		t.Fatalf("contact steps %v; want none", calls)
	}
}

// TestPeerSeenAsksFirstThenIntroduces: a node of the user that is no swarm
// member is asked for its contract over the network before anything is pushed;
// a valid contract makes the node push its active contract and schedule a sync.
func TestPeerSeenAsksFirstThenIntroduces(t *testing.T) {
	f := newContactFixture(t)

	f.mod.onPeerSeen(f.seen(f.peerID, f.userID))

	calls := f.calls.list()
	if len(calls) != 2 || calls[0].op != "contract" || calls[1].op != "push" {
		t.Fatalf("contact steps %v; want a contract read, then a push", calls)
	}
	for _, c := range calls {
		if !c.peer.IsEqual(f.peerID) || !c.zone.Is(astral.ZoneNetwork) {
			t.Fatalf("%v to %v in zone %v; want the peer in the Network zone", c.op, c.peer, c.zone)
		}
	}
	if calls[1].object != f.mod.ActiveContract() {
		t.Fatalf("pushed %v; want the active contract", calls[1].object)
	}

	select {
	case task := <-f.scheduler.tasks:
		sync, ok := task.(*SyncNodesTask)
		if !ok || !sync.remoteIdentity.IsEqual(f.peerID) {
			t.Fatalf("scheduled %v; want a sync with the peer", task)
		}
	default:
		t.Fatal("scheduled no sync")
	}
	if next, _ := f.nextAttempt(f.peerID); !next.Equal(f.now.Add(dialInterval)) {
		t.Fatalf("next attempt at %v; want %v", next, f.now.Add(dialInterval))
	}
}

// TestPeerSeenContactsOncePerInterval: a second PeerSeen inside dialInterval
// reads no contract; one after it reads the contract again.
func TestPeerSeenContactsOncePerInterval(t *testing.T) {
	f := newContactFixture(t)
	start := f.now

	f.mod.onPeerSeen(f.seen(f.peerID, f.userID))
	f.now = start.Add(dialInterval - time.Second)
	f.mod.onPeerSeen(f.seen(f.peerID, f.userID))
	if n := contractReads(f); n != 1 {
		t.Fatalf("read the contract %v times inside the interval; want once", n)
	}

	f.now = start.Add(dialInterval)
	f.mod.onPeerSeen(f.seen(f.peerID, f.userID))
	if n := contractReads(f); n != 2 {
		t.Fatalf("read the contract %v times after the interval; want twice", n)
	}
}

func contractReads(f *contactFixture) (n int) {
	for _, c := range f.calls.list() {
		if c.op == "contract" {
			n++
		}
	}
	return n
}

// badContractAnswers are the answers to user.contract that fail the check.
func badContractAnswers() map[string]func(f *contactFixture) (*auth.SignedContract, error) {
	return map[string]func(f *contactFixture) (*auth.SignedContract, error){
		"another issuer": func(f *contactFixture) (*auth.SignedContract, error) {
			return nodeContract(astral.GenerateIdentity(), f.peerID), nil
		},
		"another subject": func(f *contactFixture) (*auth.SignedContract, error) {
			return nodeContract(f.userID, astral.GenerateIdentity()), nil
		},
		"relay-for contract": func(f *contactFixture) (*auth.SignedContract, error) {
			return relayForContract(f.userID, f.peerID), nil
		},
		"forged issuer signature": func(f *contactFixture) (*auth.SignedContract, error) {
			c := nodeContract(f.userID, f.peerID)
			c.IssuerSig = forgedSig()
			return c, nil
		},
		"forged subject signature": func(f *contactFixture) (*auth.SignedContract, error) {
			c := nodeContract(f.userID, f.peerID)
			c.SubjectSig = forgedSig()
			return c, nil
		},
		"expired": func(f *contactFixture) (*auth.SignedContract, error) {
			c := nodeContract(f.userID, f.peerID)
			c.ExpiresAt = astral.Time(f.now.Add(-time.Second))
			return c, nil
		},
		"subject expelled meanwhile": func(f *contactFixture) (*auth.SignedContract, error) {
			if err := f.mod.db.StoreExpulsion(sampleSigned(f.userID, f.peerID)); err != nil {
				return nil, err
			}
			return nodeContract(f.userID, f.peerID), nil
		},
		"empty permit": func(f *contactFixture) (*auth.SignedContract, error) {
			c := nodeContract(f.userID, f.peerID)
			c.Permits = append(c.Permits, nil)
			return c, nil
		},
		"no contract": func(*contactFixture) (*auth.SignedContract, error) { return nil, nil },
		"query error": func(*contactFixture) (*auth.SignedContract, error) {
			return nil, errors.New("no route to the peer")
		},
	}
}

// TestPeerSeenRefusesABadContract: a contract that fails the check, or no
// answer, pushes nothing of this node's own and holds off the next attempt for
// dialIntervalAfterRefusal.
func TestPeerSeenRefusesABadContract(t *testing.T) {
	for name, answer := range badContractAnswers() {
		t.Run(name, func(t *testing.T) {
			f := newContactFixture(t)
			f.answer = answer
			start := f.now

			f.mod.onPeerSeen(f.seen(f.peerID, f.userID))
			f.requireRefused(t)

			f.now = start.Add(dialIntervalAfterRefusal - time.Second)
			f.mod.onPeerSeen(f.seen(f.peerID, f.userID))
			if n := contractReads(f); n != 1 {
				t.Fatalf("read the contract %v times inside the hour; want once", n)
			}
		})
	}
}

// TestPeerSeenRetriesAfterARefusal: the contract is read again once
// dialIntervalAfterRefusal has passed.
func TestPeerSeenRetriesAfterARefusal(t *testing.T) {
	f := newContactFixture(t)
	f.answer = func(*contactFixture) (*auth.SignedContract, error) { return nil, errors.New("unreachable") }
	start := f.now

	f.mod.onPeerSeen(f.seen(f.peerID, f.userID))
	f.now = start.Add(dialIntervalAfterRefusal)
	f.mod.onPeerSeen(f.seen(f.peerID, f.userID))

	if n := contractReads(f); n != 2 {
		t.Fatalf("read the contract %v times; want twice", n)
	}
}

// TestPeerSeenFullDialMap: a full dial map ignores a node it does not hold.
func TestPeerSeenFullDialMap(t *testing.T) {
	f := newContactFixture(t)
	for range dialMapCap {
		f.mod.dials.reserve(astral.GenerateIdentity(), f.now)
	}

	f.mod.onPeerSeen(f.seen(f.peerID, f.userID))

	if calls := f.calls.list(); len(calls) != 0 {
		t.Fatalf("contact steps %v; want none", calls)
	}
}

// TestReceivePeerSeenEvent: a PeerSeen event that this node sent starts a
// contact; one that another node sent starts none.
func TestReceivePeerSeenEvent(t *testing.T) {
	f := newContactFixture(t)
	forged := astral.GenerateIdentity()

	foreign := &recordingDrop{sender: astral.GenerateIdentity(), object: &events.Event{Data: f.seen(forged, f.userID)}}
	if err := f.mod.ReceiveObject(foreign); err != nil {
		t.Fatalf("ReceiveObject: %v", err)
	}
	local := &recordingDrop{sender: f.nodeID, object: &events.Event{Data: f.seen(f.peerID, f.userID)}}
	if err := f.mod.ReceiveObject(local); err != nil {
		t.Fatalf("ReceiveObject: %v", err)
	}

	select {
	case peer := <-f.calls.reads:
		if !peer.IsEqual(f.peerID) {
			t.Fatalf("read the contract of %v; want the locally reported peer", peer)
		}
	case <-time.After(effectTimeout):
		t.Fatal("the local PeerSeen started no contact")
	}
	// note: the foreign event started no goroutine, so a short wait suffices to catch a leak.
	select {
	case peer := <-f.calls.reads:
		t.Fatalf("read the contract of %v from a forged event", peer)
	case <-time.After(100 * time.Millisecond):
	}
}

// TestSiblingSeenNearbyLinksDuringBackoff: a swarm member seen nearby while its
// link task waits out a backoff is dialed within wakeDialBound of the event.
func TestSiblingSeenNearbyLinksDuringBackoff(t *testing.T) {
	lf := newLinkTaskFixture(failEvery, false)
	f := newContactFixture(t)
	f.mod.Nodes = lf.nodes
	f.mod.Scheduler = lf.scheduler
	lf.task.mod = f.mod
	lf.task.Target = f.peerID
	f.addMember(f.peerID)

	done, cancel := lf.run(t)
	lf.requireAttempt(t, 1, effectTimeout)
	lf.requireBackoff(t)
	f.mod.addSibling(f.peerID, cancel, lf.task.Wake)

	f.mod.onPeerSeen(f.seen(f.peerID, f.userID))
	lf.requireAttempt(t, 2, wakeDialBound)

	cancel()
	requireCanceled(t, done)
	if calls := f.calls.list(); len(calls) != 0 {
		t.Fatalf("contact steps %v; want none for a sibling", calls)
	}
}

// keepScheduler records every scheduled task and keeps it scheduled without running it.
type keepScheduler struct {
	scheduler.Module
	tasks chan scheduler.Task
}

func (s *keepScheduler) Schedule(task scheduler.Task, _ ...scheduler.Done) (scheduler.ScheduledTask, error) {
	s.tasks <- task
	return keptTask{}, nil
}

// keptTask is a scheduled task that never runs.
type keptTask struct {
	scheduler.ScheduledTask
}

func (keptTask) Cancel() {}

// TestSiblingLinkerRegistersTheTaskWake: the link task the sibling linker
// starts for a member is the one a PeerSeen of that member wakes.
func TestSiblingLinkerRegistersTheTaskWake(t *testing.T) {
	f := newContactFixture(t)
	f.addMember(f.peerID)
	sched := &keepScheduler{tasks: make(chan scheduler.Task, 4)}
	f.mod.Scheduler = sched

	f.mod.runSiblingLinker()
	var link *MaintainLinkTask
	select {
	case task := <-sched.tasks:
		link, _ = task.(*MaintainLinkTask)
	default:
	}
	if link == nil || !link.Target.IsEqual(f.peerID) {
		t.Fatal("the sibling linker started no link task for the member")
	}

	f.mod.onPeerSeen(f.seen(f.peerID, f.userID))

	select {
	case <-link.wake:
	default:
		t.Fatal("the PeerSeen did not wake the member's link task")
	}
	if !link.actionRequired.Load() {
		t.Fatal("the woken link task needs no action; want a dial")
	}
}
