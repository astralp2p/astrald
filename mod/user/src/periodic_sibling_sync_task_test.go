package user

import (
	"slices"
	"testing"
	"time"

	"github.com/astralp2p/astral-go/api/auth"
	"github.com/astralp2p/astral-go/api/user"
	"github.com/astralp2p/astral-go/astral"
	"github.com/astralp2p/astral-go/astral/log"
	objectsmod "github.com/astralp2p/astrald/mod/objects"
)

// activePusher is an objects module that records every push of a swarm
// membership contract issued to the node `self`, which is the active contract a
// SyncNodesTask pushes first, and answers every push with success.
type activePusher struct {
	objectsmod.Module
	self   *astral.Identity
	pushed chan string
}

func newActivePusher(self *astral.Identity) *activePusher {
	return &activePusher{self: self, pushed: make(chan string, 64)}
}

func (p *activePusher) Push(_ *astral.Context, target *astral.Identity, obj astral.Object) error {
	sc, ok := obj.(*auth.SignedContract)
	if !ok || !sc.Subject.IsEqual(p.self) || len(sc.HasPermit(user.TierMember.Action())) == 0 {
		return nil
	}
	p.pushed <- target.String()
	return nil
}

// await returns the targets of the next n active contract pushes.
func (p *activePusher) await(t *testing.T, n int) (targets []string) {
	t.Helper()
	for len(targets) < n {
		select {
		case target := <-p.pushed:
			targets = append(targets, target)
		case <-time.After(effectTimeout):
			t.Fatalf("got %d active contract pushes %v; want %d", len(targets), targets, n)
		}
	}
	return targets
}

// requireNone fails on any further active contract push within quiet.
func (p *activePusher) requireNone(t *testing.T) {
	t.Helper()
	select {
	case target := <-p.pushed:
		t.Fatalf("pushed the active contract to %v; want no further sync", target)
	case <-time.After(quiet):
	}
}

// syncModule builds node nodeID of user userID, with the linked siblings and
// the app contract it hosts, whose objects module is objects.
func syncModule(t *testing.T, ctx *astral.Context, userID, nodeID *astral.Identity, objects objectsmod.Module, linked ...*astral.Identity) (mod *Module, app *auth.SignedContract) {
	t.Helper()

	app = relayForContract(astral.GenerateIdentity(), nodeID)

	index := &indexAuth{}
	index.seed(nodeContract(userID, nodeID))
	index.seed(app)
	for _, sib := range linked {
		index.seed(nodeContract(userID, sib))
	}

	db := testDB(t)
	// note: an sqlite :memory: database is per connection; one connection keeps the goroutines of the siblings' syncs on the same database.
	sqlDB, err := db.DB.DB()
	if err != nil {
		t.Fatalf("sql db: %v", err)
	}
	sqlDB.SetMaxOpenConns(1)

	mod = &Module{
		Deps: Deps{Auth: index, Objects: objects, Nodes: &linkedNodes{linked: linked}},
		ctx:  ctx,
		node: &stepNode{id: nodeID, steps: &syncSteps{}},
		log:  log.New(nodeID),
		db:   db,
		file: FileConfig{SiblingSyncInterval: testSyncInterval},
	}
	if err := mod.config.ActiveContract.Set(nil, nodeContract(userID, nodeID)); err != nil {
		t.Fatalf("seed active contract: %v", err)
	}
	useIndexSwarm(t, mod, userID, index)
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

func requireDone(t *testing.T, done <-chan struct{}) {
	t.Helper()
	select {
	case <-done:
	case <-time.After(effectTimeout):
		t.Fatal("the periodic sibling sync did not return")
	}
}

// testSyncInterval is the sibling sync period of the tests: longer than quiet,
// so a test can see that nothing syncs before the first tick.
const testSyncInterval = 300 * time.Millisecond

// TestPeriodicSiblingSync_RunsPerSiblingEachTick: nothing syncs before the
// first tick; each tick runs one SyncNodesTask against every linked sibling and
// none against a member without a link.
func TestPeriodicSiblingSync_RunsPerSiblingEachTick(t *testing.T) {
	ctx, cancel := astral.NewContext(nil).WithCancel()
	defer cancel()
	b, c, unlinked := astral.GenerateIdentity(), astral.GenerateIdentity(), astral.GenerateIdentity()
	userID, nodeID := astral.GenerateIdentity(), astral.GenerateIdentity()
	pusher := newActivePusher(nodeID)
	mod, _ := syncModule(t, ctx, userID, nodeID, pusher, b, c)
	mod.Deps.Auth.(*indexAuth).seed(nodeContract(userID, unlinked))

	done := runPeriodic(mod, ctx)
	pusher.requireNone(t)

	for tick := 1; tick <= 3; tick++ {
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

	nodeID := astral.GenerateIdentity()
	bridge := &bridgePusher{sender: nodeID, peer: b.nodeID, to: b.mod}
	mod, app := syncModule(t, ctx, userID, nodeID, bridge, b.nodeID)

	// note: a receiver reports a refusal by leaving the push unindexed, not by an error.
	if err := bridge.Push(ctx, b.nodeID, app); err != nil {
		t.Fatalf("push: %v", err)
	}
	if got := b.auth.received(); len(got) != 0 {
		t.Fatalf("B indexed %d contracts before its LocalSwarm listed A; want the relay contract refused", len(got))
	}

	b.auth.seed(nodeContract(userID, nodeID))
	done := runPeriodic(mod, ctx)

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
// disable flag makes the task return at once, with no sync.
func TestPeriodicSiblingSync_DisabledReturnsWithoutSync(t *testing.T) {
	for name, file := range map[string]FileConfig{
		"negative interval": {SiblingSyncInterval: -time.Second},
		"disable flag":      {SiblingSyncInterval: testSyncInterval, SiblingSyncDisabled: true},
	} {
		t.Run(name, func(t *testing.T) {
			ctx, cancel := astral.NewContext(nil).WithCancel()
			defer cancel()
			b := astral.GenerateIdentity()
			userID, nodeID := astral.GenerateIdentity(), astral.GenerateIdentity()
			pusher := newActivePusher(nodeID)
			mod, _ := syncModule(t, ctx, userID, nodeID, pusher, b)
			mod.file = file

			requireDone(t, runPeriodic(mod, ctx))

			// two intervals: a sync that was not disabled would have started
			select {
			case target := <-pusher.pushed:
				t.Fatalf("pushed the active contract to %v; want no sync", target)
			case <-time.After(2 * testSyncInterval):
			}
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

	started := make(chan *astral.Identity, 64)
	release := make(chan struct{})
	done := make(chan struct{})
	go func() {
		defer close(done)
		mod.periodicSiblingSync(ctx, 20*time.Millisecond, func(_ *astral.Context, id *astral.Identity) {
			started <- id
			<-release
		})
	}()

	select {
	case id := <-started:
		if !id.IsEqual(sib) {
			t.Fatalf("synced %v; want %v", id, sib)
		}
	case <-time.After(effectTimeout):
		t.Fatal("the first tick started no sync")
	}

	// quiet spans about ten more ticks
	select {
	case id := <-started:
		t.Fatalf("a later tick started a second sync with %v while the first runs", id)
	case <-time.After(quiet):
	}

	close(release)
	select {
	case <-started:
	case <-time.After(effectTimeout):
		t.Fatal("the sibling is never synced again after its run finished")
	}
	cancel()
	<-done
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
