package crypto

import (
	"testing"
	"time"

	"github.com/astralp2p/astral-go/astral"
	"github.com/astralp2p/astral-go/astral/log"
	"github.com/astralp2p/astrald/mod/objects"
)

// scannedRepository signals each Scan and then emits only the nil snapshot
// boundary the Repository contract requires, so indexRepo loops once and returns.
type scannedRepository struct {
	objects.Repository
	label   string
	scanned chan string
}

func (r scannedRepository) Label() string { return r.label }

func (r scannedRepository) Scan(*astral.Context, bool) (<-chan *astral.ObjectID, error) {
	r.scanned <- r.label

	out := make(chan *astral.ObjectID, 1)
	out <- nil
	close(out)
	return out, nil
}

// namedRepos answers GetRepository from a map, returning the nil interface for
// every name it does not hold — which is what the objects module does.
type namedRepos struct {
	objects.Module
	repos map[string]objects.Repository
}

func (o namedRepos) GetRepository(name string) objects.Repository {
	repo, ok := o.repos[name]
	if !ok {
		return nil
	}
	return repo
}

// TestRunSkipsMissingRepository: a configured repository the objects module does
// not register is skipped, and the repositories after it are still indexed.
//
// why: before the fix Run fell through the nil branch and handed the nil
// interface to indexRepo, whose repo.Scan panicked in an unrecovered goroutine
// and killed the process -- so this test crashes the package binary rather than
// failing when the continue is absent.
func TestRunSkipsMissingRepository(t *testing.T) {
	scanned := make(chan string, 1)

	mod := &Module{
		Deps: Deps{Objects: namedRepos{repos: map[string]objects.Repository{
			"present": scannedRepository{label: "present", scanned: scanned},
		}}},
		log: log.New(nil),
	}
	// why: the missing name is first, so a Run that does not skip it panics
	// before it ever reaches "present".
	mod.config.Repos = []string{"no-such-repo", "present"}

	ctx, cancel := astral.NewContext(nil).WithCancel()
	defer cancel()

	done := make(chan error, 1)
	go func() { done <- mod.Run(ctx) }()

	select {
	case got := <-scanned:
		if got != "present" {
			t.Fatalf("scanned %q; want present", got)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("Run never scanned the repository configured after the missing one")
	}

	cancel()

	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("Run: %v", err)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("Run did not return after its context was cancelled")
	}
}
