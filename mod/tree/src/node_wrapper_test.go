package tree

import (
	"testing"

	"github.com/astralp2p/astral-go/api/tree"
	"github.com/astralp2p/astral-go/astral"
)

func TestNodeWrapperRootPath(t *testing.T) {
	mod := newConfigureNodeStateTree(t, &recordingAuth{})

	root, ok := mod.Root().(*NodeWrapper)
	if !ok {
		t.Fatalf("Root() = %T; want *NodeWrapper", mod.Root())
	}

	if got := root.Path(); got != "/" {
		t.Fatalf("root Path() = %q; want %q", got, "/")
	}
}

func TestNodeWrapperCreatePathAtDepthOne(t *testing.T) {
	mod := newConfigureNodeStateTree(t, &recordingAuth{})
	ctx := astral.NewContext(nil)

	created, err := tree.Query(ctx, mod.Root(), "/a", true)
	if err != nil {
		t.Fatalf("create /a: %v", err)
	}

	wrap, ok := created.(*NodeWrapper)
	if !ok {
		t.Fatalf("created node is %T; want *NodeWrapper", created)
	}

	if got := wrap.Path(); got != "/a" {
		t.Fatalf("created Path() = %q; want %q", got, "/a")
	}
}

func TestNodeWrapperSubPathsAtDepthOne(t *testing.T) {
	mod := newConfigureNodeStateTree(t, &recordingAuth{})
	ctx := astral.NewContext(nil)

	for _, p := range []string{"/a", "/b", "/c"} {
		if _, err := tree.Query(ctx, mod.Root(), p, true); err != nil {
			t.Fatalf("create %s: %v", p, err)
		}
	}

	sub, err := mod.Root().Sub(ctx)
	if err != nil {
		t.Fatalf("root Sub: %v", err)
	}

	if len(sub) != 3 {
		t.Fatalf("root Sub returned %d nodes; want 3", len(sub))
	}

	for name, node := range sub {
		wrap, ok := node.(*NodeWrapper)
		if !ok {
			t.Fatalf("sub node %q is %T; want *NodeWrapper", name, node)
		}

		if got, want := wrap.Path(), "/"+name; got != want {
			t.Errorf("sub node %q Path() = %q; want %q", name, got, want)
		}
	}
}
