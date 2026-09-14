package indexing

import (
	"errors"
	"testing"
	"time"

	"github.com/astralp2p/astral-go/api/auth"
	"github.com/astralp2p/astral-go/astral"
	"github.com/astralp2p/astral-go/astral/log"
	"github.com/astralp2p/astral-go/lib/query"
	"github.com/astralp2p/astral-go/lib/routing"
	authmod "github.com/astralp2p/astrald/mod/auth"
	"github.com/astralp2p/astrald/mod/objects"
)

// grantingAuth allows everything it is asked about.
type grantingAuth struct {
	authmod.Module
}

func (grantingAuth) Authorize(*astral.Context, auth.ActionObject) bool { return true }

// unscannableRepository is a repository that exists and refuses to be scanned.
//
// why: the op reads the repository before it enables it, so GetRepository must
// answer. Failing the scan ends the sync goroutine at its first call, before it
// reaches the module's database, which this module does not have.
type unscannableRepository struct {
	objects.Repository
}

var errNoScan = errors.New("scan unavailable")

func (unscannableRepository) Scan(*astral.Context, bool) (<-chan *astral.ObjectID, error) {
	return nil, errNoScan
}

type oneRepoObjects struct {
	objects.Module
	name string
}

func (o oneRepoObjects) GetRepository(name string) objects.Repository {
	if name != o.name {
		return nil
	}
	return unscannableRepository{}
}

// TestEnableRepoEnablesAnAcceptedQuery: an accepted indexing.enable_repo puts
// the repository in the tree and acks the caller.
//
// why: nothing in mod/indexing or tests/ exercised a successful enable_repo, so
// the op that changes what the node indexes had coverage for its refusal path
// only.
func TestEnableRepoEnablesAnAcceptedQuery(t *testing.T) {
	caller := astral.GenerateIdentity()
	repos := newMemNode(nil, "")

	ctx, cancel := astral.NewContext(nil).WithTimeout(30 * time.Second)
	defer cancel()

	mod := &Module{
		Deps:  Deps{Auth: grantingAuth{}, Objects: oneRepoObjects{name: "local"}},
		repos: repos,
		ctx:   ctx,
		log:   log.New(caller),
	}

	// returned closes once the op itself has returned, so the assertions below
	// read the tree and the caller's bytes after every side effect.
	returned := make(chan struct{})
	op, err := routing.NewOp(func(ctx *astral.Context, q *routing.IncomingQuery, args opEnableRepoArgs) error {
		defer close(returned)
		return mod.OpEnableRepo(ctx, q, args)
	})
	if err != nil {
		t.Fatalf("new op: %v", err)
	}

	w := &discardWriter{}
	q := astral.Launch(query.New(caller, caller, "indexing.enable_repo?repo=local", nil))

	if _, err = op.RouteQuery(ctx, q, w); err != nil {
		t.Fatalf("route indexing.enable_repo: %v", err)
	}

	select {
	case <-returned:
	case <-time.After(10 * time.Second):
		t.Fatal("indexing.enable_repo did not return")
	}

	subs, err := repos.Sub(ctx)
	if err != nil {
		t.Fatalf("read repos: %v", err)
	}
	if _, enabled := subs["local"]; !enabled {
		t.Fatal("repository local is not enabled after an accepted enable_repo")
	}
	if n := w.written(); n == 0 {
		t.Fatal("an accepted enable_repo wrote nothing to the caller; want an ack")
	}
}
