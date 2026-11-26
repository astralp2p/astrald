package user

import (
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/astralp2p/astral-go/api/auth"
	"github.com/astralp2p/astral-go/astral"
	"github.com/astralp2p/astral-go/astral/log"
	authmod "github.com/astralp2p/astrald/mod/auth"
	nodesmod "github.com/astralp2p/astrald/mod/nodes"
	"github.com/astralp2p/astrald/mod/scheduler"
)

// linkTaskScheduler records the scheduled link task of each target and the
// target of each scheduled sibling sync, and runs none.
type linkTaskScheduler struct {
	scheduler.Module

	mu    sync.Mutex
	tasks map[string]*cancelTask
	syncs []*astral.Identity
}

// synced returns the targets of the scheduled sibling syncs.
func (s *linkTaskScheduler) synced() []*astral.Identity {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]*astral.Identity(nil), s.syncs...)
}

func (s *linkTaskScheduler) Schedule(task scheduler.Task, _ ...scheduler.Done) (scheduler.ScheduledTask, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if sync, ok := task.(*SyncNodesTask); ok {
		s.syncs = append(s.syncs, sync.remoteIdentity)
		return &cancelTask{}, nil
	}
	link, ok := task.(*MaintainLinkTask)
	if !ok {
		return nil, errors.New("linkTaskScheduler runs link and sync tasks only")
	}

	t := &cancelTask{}
	s.tasks[link.Target.String()] = t
	return t, nil
}

// task returns the link task scheduled for id, or nil.
func (s *linkTaskScheduler) task(id *astral.Identity) *cancelTask {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.tasks[id.String()]
}

// cancelTask is a scheduled task that records its cancel.
type cancelTask struct {
	scheduler.ScheduledTask
	cancelled atomic.Bool
}

func (t *cancelTask) Cancel() { t.cancelled.Store(true) }

// learnNodes is a nodes module that records every LearnMemberEndpoints call
// and reports the identities in linked as linked.
type learnNodes struct {
	nodesmod.Module
	learned chan *astral.Identity
	linked  sync.Map
}

func (n *learnNodes) LearnMemberEndpoints(subject *astral.Identity) { n.learned <- subject }

func (n *learnNodes) IsLinked(id *astral.Identity) bool {
	_, ok := n.linked.Load(id.String())
	return ok
}

// linkerEnv is a module whose swarm is the subjects of e.query.found.
type linkerEnv struct {
	*memberEnv
	sched *linkTaskScheduler
	nodes *learnNodes
}

func newLinkerEnv(t *testing.T, members ...*astral.Identity) *linkerEnv {
	e := &linkerEnv{memberEnv: newMemberEnv(t), sched: &linkTaskScheduler{tasks: map[string]*cancelTask{}},
		nodes: &learnNodes{learned: make(chan *astral.Identity, 64)}}
	nodeID := astral.GenerateIdentity()
	e.mod.node = &identityNode{id: nodeID}
	e.mod.log = log.New(nodeID)
	e.mod.Scheduler = e.sched
	e.mod.Nodes = e.nodes
	e.setSwarm(t, append(members, nodeID)...)
	return e
}

func (e *linkerEnv) setSwarm(t *testing.T, members ...*astral.Identity) {
	e.query.found = nil
	for _, m := range members {
		e.query.found = append(e.query.found, memberContract(t, e.issuer, m))
	}
}

// requireLinked fails unless id has a registered, running link task.
func (e *linkerEnv) requireLinked(t *testing.T, id *astral.Identity) {
	t.Helper()
	if _, ok := e.mod.sibs.Get(id.String()); !ok {
		t.Fatalf("%v has no registered link task", id)
	}
	if task := e.sched.task(id); task == nil || task.cancelled.Load() {
		t.Fatalf("the link task of %v is missing or stopped", id)
	}
}

// requireStopped fails unless the link task of id is cancelled and unregistered.
func (e *linkerEnv) requireStopped(t *testing.T, id *astral.Identity) {
	t.Helper()
	if _, ok := e.mod.sibs.Get(id.String()); ok {
		t.Fatalf("%v still has a registered link task", id)
	}
	if task := e.sched.task(id); task == nil || !task.cancelled.Load() {
		t.Fatalf("the link task of %v was not cancelled", id)
	}
}

// TestSiblingLinkerStopsTasksOfFormerMembers: a linker run after a member left
// the swarm stops that member's link task and keeps the others' tasks.
func TestSiblingLinkerStopsTasksOfFormerMembers(t *testing.T) {
	a, b := astral.GenerateIdentity(), astral.GenerateIdentity()
	e := newLinkerEnv(t, a, b)

	e.mod.runSiblingLinker()
	e.requireLinked(t, a)
	e.requireLinked(t, b)
	kept := e.sched.task(a)

	e.setSwarm(t, a, e.mod.node.Identity())
	e.mod.runSiblingLinker()

	e.requireStopped(t, b)
	e.requireLinked(t, a)
	if e.sched.task(a) != kept {
		t.Fatal("the linker replaced the link task of a current member")
	}
}

// TestClearedContractStopsEverySiblingTask: clearing the active contract stops
// the link task of every sibling.
func TestClearedContractStopsEverySiblingTask(t *testing.T) {
	a, b := astral.GenerateIdentity(), astral.GenerateIdentity()
	e := newLinkerEnv(t, a, b)
	e.mod.runSiblingLinker()

	e.mod.onActiveContractChanged(nil)

	e.requireStopped(t, a)
	e.requireStopped(t, b)
}

// TestSiblingLinkerKeepsTasksWhenTheSwarmQueryFails: a failed contract query
// stops no link task.
func TestSiblingLinkerKeepsTasksWhenTheSwarmQueryFails(t *testing.T) {
	a := astral.GenerateIdentity()
	e := newLinkerEnv(t, a)
	e.mod.runSiblingLinker()

	e.query.err = errors.New("query failed")
	e.mod.runSiblingLinker()

	e.requireLinked(t, a)
}

// gatedQuery holds every Find until open is closed and reports the first one on entered.
type gatedQuery struct {
	*memberQuery
	entered chan struct{}
	open    chan struct{}
	once    sync.Once
}

func (q *gatedQuery) WithIssuer(*astral.Identity) authmod.ContractQueryBuilder  { return q }
func (q *gatedQuery) WithSubject(*astral.Identity) authmod.ContractQueryBuilder { return q }
func (q *gatedQuery) WithAction(...astral.Object) authmod.ContractQueryBuilder  { return q }
func (q *gatedQuery) Find(ctx *astral.Context) ([]*auth.SignedContract, error) {
	q.once.Do(func() { close(q.entered) })
	<-q.open
	return q.memberQuery.Find(ctx)
}

type gatedAuth struct {
	authmod.Module
	query *gatedQuery
}

func (a *gatedAuth) SignedContracts() authmod.ContractQueryBuilder { return a.query }

// TestClearedContractDuringALinkerRunStopsEverySiblingTask: a linker run that
// read the swarm before the contract was cleared leaves no link task behind.
func TestClearedContractDuringALinkerRunStopsEverySiblingTask(t *testing.T) {
	a, b := astral.GenerateIdentity(), astral.GenerateIdentity()
	e := newLinkerEnv(t, a, b)
	q := &gatedQuery{memberQuery: e.query, entered: make(chan struct{}), open: make(chan struct{})}
	e.mod.Auth = &gatedAuth{query: q}
	useContractSwarm(e.mod, e.issuer, func() ([]*auth.SignedContract, error) { return q.Find(nil) })

	linked := make(chan struct{})
	go func() { e.mod.runSiblingLinker(); close(linked) }()
	<-q.entered

	cleared := make(chan struct{})
	go func() { e.mod.onActiveContractChanged(nil); close(cleared) }()
	select {
	case <-cleared:
	case <-time.After(100 * time.Millisecond):
	}
	close(q.open)
	<-linked
	<-cleared

	e.requireStopped(t, a)
	e.requireStopped(t, b)
}

// learned returns the identities LearnMemberEndpoints was called for within
// wait, as a set of strings.
func (e *linkerEnv) learned(wait time.Duration) map[string]int {
	got := map[string]int{}
	deadline := time.After(wait)
	for {
		select {
		case id := <-e.nodes.learned:
			got[id.String()]++
		case <-deadline:
			return got
		}
	}
}

// TestSiblingLinkerLearnsTheEndpointsOfANewMember: the run that first lists a
// member asks mod/nodes to learn that member's endpoints, once, and never this
// node's own; a later run that lists the same members asks for nobody.
//
// why: a membership reaches a node only through SwarmState sync, which carries
// no endpoints, so without the pull two Members joined through one carrier
// never learn each other's endpoints and never link (swarm-four-siblings).
func TestSiblingLinkerLearnsTheEndpointsOfANewMember(t *testing.T) {
	a := astral.GenerateIdentity()
	e := newLinkerEnv(t, a)

	e.mod.runSiblingLinker()
	if got := e.learned(200 * time.Millisecond); len(got) != 1 || got[a.String()] != 1 {
		t.Fatalf("first run learned %v; want only %v, once", got, a)
	}

	b := astral.GenerateIdentity()
	e.setSwarm(t, a, b, e.mod.node.Identity())
	e.mod.runSiblingLinker()
	if got := e.learned(200 * time.Millisecond); len(got) != 1 || got[b.String()] != 1 {
		t.Fatalf("run that added b learned %v; want only %v, once", got, b)
	}

	e.mod.runSiblingLinker()
	if got := e.learned(200 * time.Millisecond); len(got) != 0 {
		t.Fatalf("run with no new member learned %v; want none", got)
	}
}

// TestSiblingLinkerSyncsWithANewLinkedMember: the run that first lists a
// member this node is already linked to schedules one sibling sync with it,
// and none with an unlinked new member.
//
// why: the LinkCreatedEvent sync skipped the member, which was not one yet
// (a joiner and its carrier), and the sync carries the swarm's relay contracts
// (relay-contract-forwarding).
func TestSiblingLinkerSyncsWithANewLinkedMember(t *testing.T) {
	linked, unlinked := astral.GenerateIdentity(), astral.GenerateIdentity()
	e := newLinkerEnv(t, linked, unlinked)
	e.nodes.linked.Store(linked.String(), true)

	e.mod.runSiblingLinker()
	e.mod.runSiblingLinker()

	got := e.sched.synced()
	if len(got) != 1 || !got[0].IsEqual(linked) {
		t.Fatalf("scheduled syncs with %v; want one with %v", got, linked)
	}
}
