package auth

import (
	"bytes"
	"testing"
	"time"

	"github.com/astralp2p/astral-go/api/auth"
	cryptoapi "github.com/astralp2p/astral-go/api/crypto"
	"github.com/astralp2p/astral-go/api/objects"
	"github.com/astralp2p/astral-go/astral"
	"github.com/astralp2p/astral-go/astral/log"
	cryptomod "github.com/astralp2p/astrald/mod/crypto"
	objectsmod "github.com/astralp2p/astrald/mod/objects"
)

// noRepos answers GetRepository with the nil interface for every name, which is
// what the objects module does for a name it does not hold.
type noRepos struct {
	objectsmod.Module
}

func (noRepos) GetRepository(string) objectsmod.Repository { return nil }

// TestIndexerReturnsWithoutLocalRepository: an objects module without the local
// repository ends the indexer instead of crashing it.
//
// why: before the fix the indexer called Scan on the nil interface. Run starts
// the indexer in its own goroutine, outside the module runner's recover, so the
// panic killed the node.
func TestIndexerReturnsWithoutLocalRepository(t *testing.T) {
	mod := &Module{
		Deps: Deps{Objects: noRepos{}},
		log:  log.New(nil),
	}

	done := make(chan struct{})
	go func() {
		defer close(done)
		mod.indexer(astral.NewContext(nil))
	}()

	select {
	case <-done:
	case <-time.After(10 * time.Second):
		t.Fatal("indexer did not return without a local repository")
	}
}

// followRepository scans a fixed list of IDs and records the context of the scan.
type followRepository struct {
	objectsmod.Repository
	ids  []*astral.ObjectID
	scan *astral.Context
}

func (r *followRepository) Scan(ctx *astral.Context, _ bool) (<-chan *astral.ObjectID, error) {
	r.scan = ctx

	out := make(chan *astral.ObjectID, len(r.ids)+1)
	for _, id := range r.ids {
		out <- id
	}
	out <- nil
	close(out)
	return out, nil
}

// followObjects answers the three calls of the follower and counts them.
// Probe answers probeType for every object; Load answers loaded.
type followObjects struct {
	objectsmod.Module
	repo      *followRepository
	probeType string
	loaded    astral.Object

	probes, loads int
	probeCtx      *astral.Context
	loadCtx       *astral.Context
}

func (o *followObjects) GetRepository(string) objectsmod.Repository { return o.repo }
func (o *followObjects) ReadDefault() objectsmod.Repository         { return o.repo }

func (o *followObjects) Probe(ctx *astral.Context, _ objectsmod.Repository, _ *astral.ObjectID) (*objects.Probe, error) {
	o.probes++
	o.probeCtx = ctx
	return &objects.Probe{Type: astral.String8(o.probeType)}, nil
}

func (o *followObjects) Load(ctx *astral.Context, _ objectsmod.Repository, _ *astral.ObjectID) (astral.Object, error) {
	o.loads++
	o.loadCtx = ctx
	return o.loaded, nil
}

// acceptAll stands in for the crypto module: every signature verifies.
type acceptAll struct {
	cryptomod.Module
}

func (acceptAll) Verify(*cryptoapi.PublicKey, *cryptoapi.Signature, cryptoapi.SignableTextObject) error {
	return nil
}

func followModule(t *testing.T, o *followObjects) *Module {
	t.Helper()

	mod := testModule(t)
	mod.Deps = Deps{Objects: o, Crypto: acceptAll{}}
	return mod
}

func objectIDOfSize(size uint64) *astral.ObjectID {
	return &astral.ObjectID{Size: size, Hash: [32]byte{1}}
}

func newFollowObjects(probeType string, loaded astral.Object, sizes ...uint64) *followObjects {
	repo := &followRepository{}
	for _, size := range sizes {
		repo.ids = append(repo.ids, objectIDOfSize(size))
	}
	return &followObjects{repo: repo, probeType: probeType, loaded: loaded}
}

// TestIndexerSkipsObjectAboveSizeCap: an object above 64 KiB is neither probed
// nor loaded; one of exactly 64 KiB is still probed.
func TestIndexerSkipsObjectAboveSizeCap(t *testing.T) {
	o := newFollowObjects("not.a.contract", nil, maxContractSize+1)
	followModule(t, o).indexer(astral.NewContext(nil))

	if o.probes != 0 || o.loads != 0 {
		t.Fatalf("object above the cap: %d probes, %d loads; want 0, 0", o.probes, o.loads)
	}

	o = newFollowObjects("not.a.contract", nil, maxContractSize)
	followModule(t, o).indexer(astral.NewContext(nil))

	if o.probes != 1 {
		t.Fatalf("object at the cap: %d probes; want 1", o.probes)
	}
}

// TestIndexerLoadsOnlyContracts: an object whose probed type is not a signed
// contract is never loaded.
func TestIndexerLoadsOnlyContracts(t *testing.T) {
	o := newFollowObjects("some.other.type", nil, 100, 200)
	followModule(t, o).indexer(astral.NewContext(nil))

	if o.probes != 2 || o.loads != 0 {
		t.Fatalf("non-contract objects: %d probes, %d loads; want 2, 0", o.probes, o.loads)
	}
}

// TestIndexerStillIndexesContractInLocal: a signed contract in local is probed,
// loaded, and stored.
func TestIndexerStillIndexesContractInLocal(t *testing.T) {
	sc := signedContract(astral.GenerateIdentity(), astral.GenerateIdentity(), time.Hour, permit(0))
	id, err := astral.ResolveObjectID(sc)
	if err != nil {
		t.Fatalf("resolve object id: %v", err)
	}

	o := newFollowObjects((&auth.SignedContract{}).ObjectType(), sc, id.Size)
	mod := followModule(t, o)
	mod.indexer(astral.NewContext(nil))

	if o.loads != 1 {
		t.Fatalf("contract: %d loads; want 1", o.loads)
	}
	if !mod.db.contractExists(id) {
		t.Fatal("the contract was not indexed")
	}
}

// TestIndexerRunsUnderHousekeeping: the scan, the probe and the load of the
// follower carry the housekeeping mark, and the scan excludes the network zone.
//
// note: reads are counted by the tracked wrapper only in part 2b; until then the
// mark is the testable fact, not an unchanged read time.
func TestIndexerRunsUnderHousekeeping(t *testing.T) {
	sc := signedContract(astral.GenerateIdentity(), astral.GenerateIdentity(), time.Hour, permit(0))
	o := newFollowObjects((&auth.SignedContract{}).ObjectType(), sc, 100)
	followModule(t, o).indexer(astral.NewContext(nil))

	for name, ctx := range map[string]*astral.Context{"scan": o.repo.scan, "probe": o.probeCtx, "load": o.loadCtx} {
		if ctx == nil || !objectsmod.IsHousekeeping(ctx) {
			t.Errorf("%s context is not marked housekeeping", name)
		}
	}
	if o.repo.scan.Zone().Is(astral.ZoneNetwork) {
		t.Error("the scan context still includes the network zone")
	}
}

// TestProbedContractTypeEqualsObjectType: the type a probe reads from an encoded
// contract equals the ObjectType the follower compares against.
func TestProbedContractTypeEqualsObjectType(t *testing.T) {
	sc := signedContract(astral.GenerateIdentity(), astral.GenerateIdentity(), time.Hour, permit(0))

	var buf bytes.Buffer
	if _, err := astral.Encode(&buf, sc, astral.Canonical()); err != nil {
		t.Fatalf("encode: %v", err)
	}

	// note: the same header read as objects Probe: a stamp, then the type.
	r := bytes.NewReader(buf.Bytes())
	if _, err := (&astral.Stamp{}).ReadFrom(r); err != nil {
		t.Fatalf("read stamp: %v", err)
	}
	var typ astral.ObjectType
	if _, err := typ.ReadFrom(r); err != nil {
		t.Fatalf("read type: %v", err)
	}

	if typ.String() != (&auth.SignedContract{}).ObjectType() {
		t.Fatalf("probed type %q; want %q", typ.String(), (&auth.SignedContract{}).ObjectType())
	}
}
