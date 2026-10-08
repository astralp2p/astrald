package tree

import (
	"context"
	"fmt"
	"sync"
	"testing"

	"github.com/astralp2p/astral-go/astral"
	treemod "github.com/astralp2p/astrald/mod/tree"
)

// Paths with a shared prefix, built in parallel as modules bind their settings at load, all land
// under that prefix.
func TestQueryParallelSharedPrefix(t *testing.T) {
	ctx := astral.NewContext(context.Background())
	mod := newConfigureNodeStateTree(t, &recordingAuth{})

	const modules = 8
	var wg sync.WaitGroup
	errs := make(chan error, modules)
	for i := range modules {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, err := treemod.Query(ctx, mod.Root(), fmt.Sprintf("/mod/m%d/settings/listen", i), true)
			errs <- err
		}()
	}
	wg.Wait()
	close(errs)

	for err := range errs {
		if err != nil {
			t.Fatalf("Query: %v", err)
		}
	}

	top, err := mod.Root().Sub(ctx)
	if err != nil {
		t.Fatalf("sub /: %v", err)
	}
	if len(top) != 1 || top["mod"] == nil {
		t.Errorf("root holds %v, want /mod alone", keys(top))
	}

	for i := range modules {
		path := fmt.Sprintf("/mod/m%d/settings/listen", i)
		if _, err := treemod.Query(ctx, mod.Root(), path, false); err != nil {
			t.Errorf("%s: %v", path, err)
		}
	}
}

func keys[V any](m map[string]V) (list []string) {
	for k := range m {
		list = append(list, k)
	}
	return
}
