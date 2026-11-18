package auth

import (
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/astralp2p/astral-go/api/auth"
	"github.com/astralp2p/astral-go/api/nodes"
	"github.com/astralp2p/astral-go/api/objects"
	"github.com/astralp2p/astral-go/astral"
	authmod "github.com/astralp2p/astrald/mod/auth"
	objectsmod "github.com/astralp2p/astrald/mod/objects"
)

// admitModule is a module with an in-memory index on one connection, whose
// signatures all verify.
func admitModule(t *testing.T) *Module {
	t.Helper()

	mod := testModule(t)
	mod.Deps.Crypto = acceptAll{}

	// note: an sqlite :memory: database is per connection; one connection keeps goroutines on the same database.
	sqlDB, err := mod.db.DB.DB()
	if err != nil {
		t.Fatalf("sql db: %v", err)
	}
	sqlDB.SetMaxOpenConns(1)
	return mod
}

func objectIDOf(t *testing.T, sc *auth.SignedContract) *astral.ObjectID {
	t.Helper()

	id, err := astral.ResolveObjectID(sc)
	if err != nil {
		t.Fatalf("resolve object id: %v", err)
	}
	return id
}

// TestAdmitterRefusalStoresNothing: a contract an admitter refuses gets no row,
// runs no OnContractIndexed callback, and its error wraps ErrNotAdmitted.
func TestAdmitterRefusalStoresNothing(t *testing.T) {
	mod := admitModule(t)
	var indexed atomic.Int32
	mod.OnContractIndexed(func(*auth.SignedContract) { indexed.Add(1) })
	refusal := errors.New("not a member")
	mod.AddAdmitter(func(*auth.SignedContract) error { return refusal })

	sc := signedContract(astral.GenerateIdentity(), astral.GenerateIdentity(), time.Hour, permit(0))
	err := mod.IndexContract(astral.NewContext(nil), sc)

	if !errors.Is(err, authmod.ErrNotAdmitted) || !errors.Is(err, refusal) {
		t.Fatalf("IndexContract: %v; want ErrNotAdmitted wrapping the refusal", err)
	}
	if mod.db.contractExists(objectIDOf(t, sc)) {
		t.Fatal("a refused contract was stored")
	}
	if n := indexed.Load(); n != 0 {
		t.Fatalf("OnContractIndexed ran %d times; want 0", n)
	}
}

// TestAdmittersAdmitIntoTheIndex: a contract every admitter accepts is stored
// and announced once; an admitter added later is asked as well.
func TestAdmittersAdmitIntoTheIndex(t *testing.T) {
	mod := admitModule(t)
	var indexed, asked atomic.Int32
	mod.OnContractIndexed(func(*auth.SignedContract) { indexed.Add(1) })
	mod.AddAdmitter(func(*auth.SignedContract) error { asked.Add(1); return nil })
	mod.AddAdmitter(func(*auth.SignedContract) error { asked.Add(1); return nil })

	sc := signedContract(astral.GenerateIdentity(), astral.GenerateIdentity(), time.Hour, permit(0))
	if err := mod.IndexContract(astral.NewContext(nil), sc); err != nil {
		t.Fatalf("IndexContract: %v", err)
	}

	if !mod.db.contractExists(objectIDOf(t, sc)) {
		t.Fatal("an admitted contract was not stored")
	}
	if indexed.Load() != 1 || asked.Load() != 2 {
		t.Fatalf("callbacks %d, admitters asked %d; want 1, 2", indexed.Load(), asked.Load())
	}
}

// TestAdmittersSkipAHeldContract: a contract the index holds already is not
// put to the admitters again, so a startup re-scan reads no other module.
func TestAdmittersSkipAHeldContract(t *testing.T) {
	mod := admitModule(t)
	sc := signedContract(astral.GenerateIdentity(), astral.GenerateIdentity(), time.Hour, permit(0))
	storeContract(t, mod, sc)

	var asked atomic.Int32
	mod.AddAdmitter(func(*auth.SignedContract) error { asked.Add(1); return errors.New("refused") })

	if err := mod.IndexContract(astral.NewContext(nil), sc); err != nil {
		t.Fatalf("IndexContract of a held contract: %v; want nil", err)
	}
	if n := asked.Load(); n != 0 {
		t.Fatalf("admitters asked %d times about a held contract; want 0", n)
	}
}

// TestAdmittersRunOutsideTheIndexLock: every admitter call finds indexMu free.
//
// why TryLock on one goroutine: an admitter run under indexMu would find it
// held, and a read that waited on indexMu from there would never return.
func TestAdmittersRunOutsideTheIndexLock(t *testing.T) {
	mod := admitModule(t)
	var calls, underLock int
	mod.AddAdmitter(func(*auth.SignedContract) error {
		calls++
		if !mod.indexMu.TryLock() {
			underLock++
			return nil
		}
		mod.indexMu.Unlock()
		return nil
	})

	sc := signedContract(astral.GenerateIdentity(), astral.GenerateIdentity(), time.Hour, permit(0))
	if err := mod.IndexContract(astral.NewContext(nil), sc); err != nil {
		t.Fatalf("IndexContract: %v", err)
	}
	if calls != 1 || underLock != 0 {
		t.Fatalf("admitter calls %d, under indexMu %d; want 1, 0", calls, underLock)
	}
}

// TestIndexingWhileTheIndexIsReadEnds: an admitter reads the index, as the user
// admitter does through LocalSwarm, while other goroutines index and read the
// index at the same time. All of it ends; run with -race.
func TestIndexingWhileTheIndexIsReadEnds(t *testing.T) {
	mod := admitModule(t)
	ctx := astral.NewContext(nil)
	mod.AddAdmitter(func(*auth.SignedContract) error {
		_, err := mod.SignedContracts().WithAction(&testAction{}).Find(ctx)
		return err
	})

	const workers, perWorker = 4, 10
	done := make(chan struct{})
	go func() {
		defer close(done)
		var wg sync.WaitGroup
		for range workers {
			wg.Add(1)
			go func() {
				defer wg.Done()
				for range perWorker {
					sc := signedContract(astral.GenerateIdentity(), astral.GenerateIdentity(), time.Hour, relayPermit())
					if err := mod.IndexContract(ctx, sc); err != nil {
						t.Errorf("IndexContract: %v", err)
					}
					if _, err := mod.SignedContracts().Find(ctx); err != nil {
						t.Errorf("Find: %v", err)
					}
				}
			}()
		}
		wg.Wait()
	}()

	select {
	case <-done:
	case <-time.After(30 * time.Second):
		t.Fatal("indexing while the index is read did not finish within 30s")
	}
}

// orderedObjects serves a fixed list of signed contracts from local, in order.
type orderedObjects struct {
	objectsmod.Module
	repo    *followRepository
	objects map[string]astral.Object
}

func newOrderedObjects(t *testing.T, contracts ...*auth.SignedContract) *orderedObjects {
	t.Helper()

	o := &orderedObjects{repo: &followRepository{}, objects: map[string]astral.Object{}}
	for _, sc := range contracts {
		id := objectIDOf(t, sc)
		o.repo.ids = append(o.repo.ids, id)
		o.objects[id.String()] = sc
	}
	return o
}

func (o *orderedObjects) GetRepository(string) objectsmod.Repository { return o.repo }
func (o *orderedObjects) ReadDefault() objectsmod.Repository         { return o.repo }

func (o *orderedObjects) Probe(*astral.Context, objectsmod.Repository, *astral.ObjectID) (*objects.Probe, error) {
	return &objects.Probe{Type: astral.String8((&auth.SignedContract{}).ObjectType())}, nil
}

func (o *orderedObjects) Load(_ *astral.Context, _ objectsmod.Repository, id *astral.ObjectID) (astral.Object, error) {
	return o.objects[id.String()], nil
}

// memberRule stands in for the user admitter: a relay-for contract is admitted
// only while the index holds a "membership" contract (permit) for its subject,
// and never for a banned subject.
func memberRule(mod *Module, banned *astral.Identity) func(*auth.SignedContract) error {
	return func(sc *auth.SignedContract) error {
		if len(sc.HasPermit(nodes.RelayForAction{}.ObjectType())) == 0 {
			return nil
		}
		if sc.Subject.IsEqual(banned) {
			return errors.New("subject expelled")
		}
		members, err := mod.SignedContracts().WithSubject(sc.Subject).WithAction(&testAction{}).Find(astral.NewContext(nil))
		if err != nil {
			return err
		}
		if len(members) == 0 {
			return errors.New("subject is not a member")
		}
		return nil
	}
}

// TestIndexerRetriesRefusedContractsAfterTheSnapshot: a relay contract that the
// repository lists before the membership contract of its subject is refused on
// the first pass and indexed by the retry at the end of the snapshot.
func TestIndexerRetriesRefusedContractsAfterTheSnapshot(t *testing.T) {
	user, host := astral.GenerateIdentity(), astral.GenerateIdentity()
	relay := signedContract(astral.GenerateIdentity(), host, time.Hour, relayPermit())
	member := signedContract(user, host, time.Hour, permit(0))

	o := newOrderedObjects(t, relay, member)
	mod := admitModule(t)
	mod.Deps.Objects = o
	mod.AddAdmitter(memberRule(mod, nil))

	mod.indexer(astral.NewContext(nil))

	if !mod.db.contractExists(objectIDOf(t, member)) {
		t.Fatal("the membership contract was not indexed")
	}
	if !mod.db.contractExists(objectIDOf(t, relay)) {
		t.Fatal("the relay contract listed before its subject's membership was not indexed after the snapshot")
	}
}

// TestIndexerKeepsAnExpelledHostOutAfterAWipe: an empty index rebuilt from a
// repository that still holds an expelled host's membership and relay
// contracts does not index the relay contract, not even on the retry.
func TestIndexerKeepsAnExpelledHostOutAfterAWipe(t *testing.T) {
	user, expelled, member := astral.GenerateIdentity(), astral.GenerateIdentity(), astral.GenerateIdentity()
	expelledRelay := signedContract(astral.GenerateIdentity(), expelled, time.Hour, relayPermit())
	memberRelay := signedContract(astral.GenerateIdentity(), member, time.Hour, relayPermit())
	contracts := []*auth.SignedContract{
		signedContract(user, expelled, time.Hour, permit(0)),
		signedContract(user, member, time.Hour, permit(0)),
		expelledRelay,
		memberRelay,
	}

	mod := admitModule(t)
	mod.Deps.Objects = newOrderedObjects(t, contracts...)
	mod.AddAdmitter(memberRule(mod, expelled))

	mod.indexer(astral.NewContext(nil))

	if mod.db.contractExists(objectIDOf(t, expelledRelay)) {
		t.Fatal("the expelled host's relay contract was indexed again")
	}
	if !mod.db.contractExists(objectIDOf(t, memberRelay)) {
		t.Fatal("a member's relay contract was not indexed")
	}
}
