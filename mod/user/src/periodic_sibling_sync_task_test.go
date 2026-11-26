package user

import (
	"slices"
	"testing"
	"time"

	"github.com/astralp2p/astral-go/api/auth"
	"github.com/astralp2p/astral-go/astral"
	"github.com/astralp2p/astral-go/astral/log"
	objectsmod "github.com/astralp2p/astrald/mod/objects"
)

// appPusher is an objects module that records every push of a relay-for
// contract whose subject is the node `self`, the app contract a SyncNodesTask
// pushes, and answers every push with success.
type appPusher struct {
	objectsmod.Module
	self   *astral.Identity
	pushed chan string
}

func newAppPusher(self *astral.Identity) *appPusher {
	return &appPusher{self: self, pushed: make(chan string, 64)}
}

func (p *appPusher) Push(_ *astral.Context, target *astral.Identity, obj astral.Object) error {
	sc, ok := obj.(*auth.SignedContract)
	if !ok || !sc.Subject.IsEqual(p.self) || !isRelayForContract(sc.Contract) {
		return nil
	}
	p.pushed <- target.String()
	return nil
}

// await returns the targets of the next n app contract pushes.
func (p *appPusher) await(t *testing.T, n int) (targets []string) {
	t.Helper()
	for len(targets) < n {
		select {
		case target := <-p.pushed:
			targets = append(targets, target)
		case <-time.After(effectTimeout):
			t.Fatalf("got %d app contract pushes %v; want %d", len(targets), targets, n)
		}
	}
	return targets
}

// requireNone fails on any further app contract push within quiet.
func (p *appPusher) requireNone(t *testing.T) {
	t.Helper()
	select {
	case target := <-p.pushed:
		t.Fatalf("pushed the app contract to %v; want no further sync", target)
	case <-time.After(quiet):
	}
}

// syncModule builds node nodeID of user userID, with the linked siblings and
// the app contract it hosts, whose objects module is objects and whose clock is
// clock.
func syncModule(t *testing.T, ctx *astral.Context, userID, nodeID *astral.Identity, objects objectsmod.Module, clock *stepClock, linked ...*astral.Identity) (mod *Module, app *auth.SignedContract) {
	t.Helper()

	app = relayForContract(astral.GenerateIdentity(), nodeID)

	index := &indexAuth{}
	index.seed(membershipContract(userID, nodeID))
	index.seed(app)
	for _, sib := range linked {
		index.seed(membershipContract(userID, sib))
	}

	db := testDB(t)
	// note: an sqlite :memory: database is per connection; one connection keeps the goroutines of the siblings' syncs on the same database.
	sqlDB, err := db.DB.DB()
	if err != nil {
		t.Fatalf("sql db: %v", err)
	}
	sqlDB.SetMaxOpenConns(1)

	mod = &Module{
		Deps:  Deps{Auth: index, Objects: objects, Nodes: &linkedNodes{linked: linked}, Tree: memTree{root: newMemNode()}},
		ctx:   ctx,
		node:  &stepNode{id: nodeID, steps: &syncSteps{}},
		log:   log.New(nodeID),
		db:    db,
		after: clock.after,
	}
	if err := mod.config.ActiveContract.Set(nil, membershipContract(userID, nodeID)); err != nil {
		t.Fatalf("seed active contract: %v", err)
	}
	useIndexSwarm(mod, userID, index)
	return mod, app
}

// runPeriodic runs the periodic sibling sync of mod and returns a channel closed when it returns.
func runPeriodic(mod *Module, ctx *astral.Context) <-chan struct{} {
	done := make(chan struct{})
	go func() {
		defer close(done)
		mod.runPeriodicSiblingSync(ctx)
	}()
	return done
}

// TestPeriodicSiblingSync_RunsPerSiblingEachTick: nothing syncs before the
// first tick; each tick runs one SyncNodesTask against every linked sibling and
// none against a member without a link; the wait before a tick is the interval
// plus at most 10 % jitter.
func TestPeriodicSiblingSync_RunsPerSiblingEachTick(t *testing.T) {
	ctx, cancel := astral.NewContext(nil).WithCancel()
	defer cancel()
	b, c, unlinked := astral.GenerateIdentity(), astral.GenerateIdentity(), astral.GenerateIdentity()
	clock := newStepClock()
	userID, nodeID := astral.GenerateIdentity(), astral.GenerateIdentity()
	pusher := newAppPusher(nodeID)
	mod, _ := syncModule(t, ctx, userID, nodeID, pusher, clock, b, c)
	mod.Deps.Auth.(*indexAuth).seed(membershipContract(userID, unlinked))

	done := runPeriodic(mod, ctx)

	for tick := 1; tick <= 3; tick++ {
		wait := clock.wait(t)
		if wait < 6*time.Hour || wait > 6*time.Hour+36*time.Minute {
			t.Fatalf("tick %d waited %v; want 6 h plus at most 10 %%", tick, wait)
		}
		pusher.requireNone(t)

		clock.step(t)
		got := pusher.await(t, 2)
		slices.Sort(got)
		want := []string{b.String(), c.String()}
		slices.Sort(want)
		if !slices.Equal(got, want) {
			t.Fatalf("tick %d synced %v; want the linked siblings %v once each", tick, got, want)
		}
	}

	cancel()
	requireDone(t, done)
}

// TestPeriodicSiblingSync_ResendsARelayContractTheSiblingRefused: B refused the
// relay-for contract of an app of A because its LocalSwarm did not list A; once
// it does, the next tick of the sync delivers the contract.
func TestPeriodicSiblingSync_ResendsARelayContractTheSiblingRefused(t *testing.T) {
	ctx, cancel := astral.NewContext(nil).WithCancel()
	defer cancel()
	userID := astral.GenerateIdentity()
	b := newReceiverFixture(t, userID)
	clock := newStepClock()

	nodeID := astral.GenerateIdentity()
	bridge := &bridgePusher{sender: nodeID, peer: b.nodeID, to: b.mod}
	mod, app := syncModule(t, ctx, userID, nodeID, bridge, clock, b.nodeID)

	// note: a receiver reports a refusal by leaving the push unindexed, not by an error.
	if err := bridge.Push(ctx, b.nodeID, app); err != nil {
		t.Fatalf("push: %v", err)
	}
	if got := b.auth.received(); len(got) != 0 {
		t.Fatalf("B indexed %d contracts before its LocalSwarm listed A; want the relay contract refused", len(got))
	}

	b.auth.seed(membershipContract(userID, nodeID))
	done := runPeriodic(mod, ctx)
	clock.wait(t)
	if got := b.auth.received(); slices.Contains(got, app) {
		t.Fatal("B holds the relay contract before the tick")
	}
	clock.step(t)

	deadline := time.After(effectTimeout)
	for !slices.Contains(b.auth.received(), app) {
		select {
		case <-deadline:
			t.Fatal("B holds no relay contract of the app after the tick")
		case <-time.After(10 * time.Millisecond):
		}
	}

	cancel()
	requireDone(t, done)
}

// TestPeriodicSiblingSync_DisabledReturnsWithoutSync: a negative interval or the
// disable flag makes the task return at once, with no wait and no sync.
func TestPeriodicSiblingSync_DisabledReturnsWithoutSync(t *testing.T) {
	for name, file := range map[string]FileConfig{
		"negative interval": {SiblingSyncInterval: -time.Second},
		"disable flag":      {SiblingSyncInterval: time.Hour, SiblingSyncDisabled: true},
	} {
		t.Run(name, func(t *testing.T) {
			ctx, cancel := astral.NewContext(nil).WithCancel()
			defer cancel()
			b := astral.GenerateIdentity()
			clock := newStepClock()
			userID, nodeID := astral.GenerateIdentity(), astral.GenerateIdentity()
			pusher := newAppPusher(nodeID)
			mod, _ := syncModule(t, ctx, userID, nodeID, pusher, clock, b)
			mod.file = file

			requireDone(t, runPeriodic(mod, ctx))

			select {
			case d := <-clock.waits:
				t.Fatalf("started a wait of %v; want none", d)
			default:
			}
			pusher.requireNone(t)
		})
	}
}

// TestPeriodicSiblingSync_SkipsASiblingWhoseSyncRuns: a sibling whose previous
// run has not finished is not started again on the next tick, and runs again
// after it finished.
func TestPeriodicSiblingSync_SkipsASiblingWhoseSyncRuns(t *testing.T) {
	ctx, cancel := astral.NewContext(nil).WithCancel()
	defer cancel()
	sib := astral.GenerateIdentity()
	mod := pushFixture(t, []*astral.Identity{sib}, []*astral.Identity{sib})
	clock := newStepClock()
	mod.after = clock.after

	started := make(chan *astral.Identity, 8)
	release := make(chan struct{})
	done := make(chan struct{})
	go func() {
		defer close(done)
		mod.periodicSiblingSync(ctx, time.Hour, func(_ *astral.Context, id *astral.Identity) {
			started <- id
			<-release
		})
	}()

	clock.wait(t)
	clock.step(t)
	select {
	case id := <-started:
		if !id.IsEqual(sib) {
			t.Fatalf("synced %v; want %v", id, sib)
		}
	case <-time.After(effectTimeout):
		t.Fatal("the first tick started no sync")
	}

	clock.wait(t)
	clock.step(t)
	select {
	case id := <-started:
		t.Fatalf("the next tick started a second sync with %v while the first runs", id)
	case <-time.After(quiet):
	}

	close(release)
	for range 200 {
		clock.wait(t)
		clock.step(t)
		select {
		case <-started:
			cancel()
			<-done
			return
		case <-time.After(20 * time.Millisecond):
		}
	}
	t.Fatal("the sibling is never synced again after its run finished")
}

// TestSiblingSyncInterval_DefaultsToSixHours pins the 6 h default, the rule that
// zero selects it, and the jitter bound of 10 %.
func TestSiblingSyncInterval_DefaultsToSixHours(t *testing.T) {
	if defaultFileConfig.SiblingSyncInterval != 6*time.Hour {
		t.Fatalf("default sibling_sync_interval = %v, want 6h", defaultFileConfig.SiblingSyncInterval)
	}
	if defaultFileConfig.SiblingSyncDisabled {
		t.Fatal("sibling_sync_disabled defaults to true; want false")
	}

	cases := []struct{ set, want time.Duration }{
		{0, 6 * time.Hour},
		{5 * time.Minute, 5 * time.Minute},
	}
	for _, c := range cases {
		mod := &Module{file: FileConfig{SiblingSyncInterval: c.set}}
		if got := mod.siblingSyncInterval(); got != c.want {
			t.Errorf("sibling_sync_interval %v: got %v, want %v", c.set, got, c.want)
		}
	}

	interval := time.Hour
	seen := map[time.Duration]struct{}{}
	for range 1000 {
		d := jitteredSiblingSyncDelay(interval)
		if d < interval || d > interval+interval/10 {
			t.Fatalf("jittered delay %v; want %v plus at most 10 %%", d, interval)
		}
		seen[d] = struct{}{}
	}
	if len(seen) < 2 {
		t.Fatal("the delay never varies; want jitter")
	}
}
