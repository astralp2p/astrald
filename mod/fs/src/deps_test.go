package fs

import (
	"testing"

	"github.com/astralp2p/astral-go/astral"
	"github.com/astralp2p/astral-go/astral/log"
	objectsmod "github.com/astralp2p/astrald/mod/objects"
)

// recordingObjects records the repositories a caller registers and answers nothing else.
type recordingObjects struct {
	objectsmod.Module
	added []string
}

func (o *recordingObjects) AddRepository(name string, _ objectsmod.Repository) error {
	o.added = append(o.added, name)
	return nil
}

func (o *recordingObjects) AddGroup(string, string) error { return nil }

// TestAddConfigRepos_FailedRepoDoesNotSkipTheRest: a read-only repo that cannot be built
// is logged and skipped, and a writable repo configured alongside it is still registered.
func TestAddConfigRepos_FailedRepoDoesNotSkipTheRest(t *testing.T) {
	logger := log.New(astral.GenerateIdentity())

	// why: the filter gates the writer, so the lines this loop logs stay out of the
	// test binary's output.
	logger.SetFilter(func(*log.Entry) bool { return false })

	// why: a relative path is refused by NewWatchRepository before it stats anything,
	// so the failing repo needs neither a directory on disk nor mod.indexer.
	repos := map[string]RepoConfig{
		"broken":   {Label: "broken", Path: "relative"},
		"writable": {Label: "writable", Path: t.TempDir(), Writable: true},
	}

	// why: Repos is a map and Go randomizes range order, so one pass visits "broken"
	// before "writable" only half the time. Repeating makes an error scoped to
	// addConfigRepos rather than to the iteration certain to show.
	for pass := 0; pass < 50; pass++ {
		store := &recordingObjects{}
		mod := &Module{Deps: Deps{Objects: store}, log: logger}
		mod.config.Repos = repos

		mod.addConfigRepos()

		if len(store.added) != 1 || store.added[0] != "writable" {
			t.Fatalf("pass %d: want [writable], got %v", pass, store.added)
		}
	}
}
