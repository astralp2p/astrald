package crypto

import (
	"errors"
	"testing"

	"github.com/astralp2p/astral-go/astral"
	"github.com/astralp2p/astral-go/astral/log"
	"github.com/astralp2p/astrald/mod/objects"
)

// recordedScan emits one small object and records the context of the scan.
type recordedScan struct {
	objects.Repository
	scan *astral.Context
}

func (r *recordedScan) Label() string { return "recorded" }

func (r *recordedScan) Scan(ctx *astral.Context, _ bool) (<-chan *astral.ObjectID, error) {
	r.scan = ctx

	out := make(chan *astral.ObjectID, 2)
	out <- &astral.ObjectID{Size: 10, Hash: [32]byte{1}}
	out <- nil
	close(out)
	return out, nil
}

// recordedLoad records the context of every Load.
type recordedLoad struct {
	objects.Module
	loads []*astral.Context
}

func (o *recordedLoad) Load(ctx *astral.Context, _ objects.Repository, _ *astral.ObjectID) (astral.Object, error) {
	o.loads = append(o.loads, ctx)
	return nil, errors.New("unreadable")
}

// TestIndexRepoRunsUnderHousekeeping: the scan and the loads of the crypto
// follower carry the housekeeping mark.
//
// note: reads are counted by the tracked wrapper only in part 2b; until then the
// mark is the testable fact, not an unchanged read time.
func TestIndexRepoRunsUnderHousekeeping(t *testing.T) {
	repo := &recordedScan{}
	o := &recordedLoad{}
	mod := &Module{Deps: Deps{Objects: o}, log: log.New(nil)}

	if err := mod.indexRepo(astral.NewContext(nil), repo); err != nil {
		t.Fatalf("indexRepo: %v", err)
	}

	if repo.scan == nil || !objects.IsHousekeeping(repo.scan) {
		t.Error("scan context is not marked housekeeping")
	}
	if len(o.loads) != 1 {
		t.Fatalf("%d loads; want 1", len(o.loads))
	}
	if !objects.IsHousekeeping(o.loads[0]) {
		t.Error("load context is not marked housekeeping")
	}
}
