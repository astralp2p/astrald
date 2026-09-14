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

// blockingAuth grants, but not before the test releases it.
//
// why: routing.Op gives an op five seconds to accept or reject; past that the
// router rejects on the op's behalf and answers the caller, while the op keeps
// running on its detached context. Holding the authorization call open past that
// deadline puts the op on the far side of a rejection the caller already has.
type blockingAuth struct {
	authmod.Module
	release chan struct{}
}

func (a *blockingAuth) Authorize(*astral.Context, auth.ActionObject) bool {
	<-a.release
	return true
}

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

// TestEnableRepoDoesNotEnableARejectedQuery: when the router rejects the query
// before the op resolves it, the caller holds a rejection — and the repository
// must not end up enabled behind it.
//
// The observed shape (2026-09-14): a caller received "query rejected (1)" after
// 5.00118s, and the next indexing.enable_repo for the same repository answered
// "node already exists".
func TestEnableRepoDoesNotEnableARejectedQuery(t *testing.T) {
	caller := astral.GenerateIdentity()
	repos := newMemNode(nil, "")

	ctx, cancel := astral.NewContext(nil).WithTimeout(30 * time.Second)
	defer cancel()

	authority := &blockingAuth{release: make(chan struct{})}
	mod := &Module{
		Deps:  Deps{Auth: authority, Objects: oneRepoObjects{name: "local"}},
		repos: repos,
		ctx:   ctx,
		log:   log.New(caller),
	}

	// returned closes once the op function itself has returned, so the assertion
	// below reads the tree after every side effect the op could apply.
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

	_, err = op.RouteQuery(ctx, q, w)

	var rejected *astral.ErrRejected
	if !errors.As(err, &rejected) {
		t.Fatalf("router answered %v; want the rejection it sends when the op misses its deadline", err)
	}

	close(authority.release)

	select {
	case <-returned:
	case <-time.After(10 * time.Second):
		t.Fatal("indexing.enable_repo did not return after its authorization was granted")
	}

	subs, err := repos.Sub(ctx)
	if err != nil {
		t.Fatalf("read repos: %v", err)
	}
	if _, enabled := subs["local"]; enabled {
		t.Fatal("repository local is enabled after the caller was told the query was rejected")
	}
	if n := w.written(); n != 0 {
		t.Fatalf("wrote %d bytes to a caller holding a rejection; want none", n)
	}
}
