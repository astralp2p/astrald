package tree

import (
	"fmt"
	"sort"
	"sync"
	"testing"

	"github.com/astralp2p/astral-go/api/tree"
	"github.com/astralp2p/astral-go/astral"
	treemod "github.com/astralp2p/astrald/mod/tree"
)

// gatedNode holds the first n calls of Sub until all n have listed the subnodes. Calls past n
// pass through.
type gatedNode struct {
	tree.Node
	mu   sync.Mutex
	n    int
	open chan struct{}
}

func (g *gatedNode) Sub(ctx *astral.Context) (map[string]tree.Node, error) {
	subs, err := g.Node.Sub(ctx)
	if wait := g.arrive(); wait != nil {
		<-wait
	}
	return subs, err
}

// arrive counts one held caller and returns the gate to wait on, or nil once the gate is open.
func (g *gatedNode) arrive() <-chan struct{} {
	g.mu.Lock()
	defer g.mu.Unlock()

	if g.n == 0 {
		return nil
	}
	g.n--
	if g.n == 0 {
		close(g.open)
	}
	return g.open
}

type bindPathConfig struct {
	Mode tree.Value[*astral.String8]
}

// Modules binding their config in parallel each land on /mod/<name>/config, never on a path
// missing the shared /mod prefix.
func TestBindPathParallelSharedPrefix(t *testing.T) {
	const binders = 8
	mod := newConfigureNodeStateTree(t, &recordingAuth{})
	// why: every binder lists an empty root before any creates /mod, so all but one lose the race.
	mod.mounts.Replace("/", &gatedNode{Node: &Node{mod: mod}, n: binders, open: make(chan struct{})})
	ctx, cancel := astral.NewContext(nil).WithCancel()
	defer cancel()

	errs := make(chan error, binders)
	var wg sync.WaitGroup
	for i := range binders {
		wg.Add(1)
		go func() {
			defer wg.Done()
			var cfg bindPathConfig
			errs <- treemod.BindPath(ctx, &cfg, mod.Root(), fmt.Sprintf("/mod/m%d/config", i), true)
		}()
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatalf("BindPath: %v", err)
		}
	}

	if got := subNames(t, ctx, mod.Root()); len(got) != 1 || got[0] != "mod" {
		t.Fatalf("root holds %v, want [mod]", got)
	}
	for i := range binders {
		path := fmt.Sprintf("/mod/m%d/config/mode", i)
		if _, err := tree.Query(ctx, mod.Root(), path, false); err != nil {
			t.Errorf("%s: %v", path, err)
		}
	}
}

func subNames(t *testing.T, ctx *astral.Context, node tree.Node) []string {
	t.Helper()

	subs, err := node.Sub(ctx)
	if err != nil {
		t.Fatalf("Sub: %v", err)
	}
	names := make([]string, 0, len(subs))
	for name := range subs {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}
