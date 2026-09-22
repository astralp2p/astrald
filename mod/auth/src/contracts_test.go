package auth

import (
	"testing"
	"time"

	"github.com/astralp2p/astral-go/astral"
	"github.com/astralp2p/astral-go/astral/log"
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
