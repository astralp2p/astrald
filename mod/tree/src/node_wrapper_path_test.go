package tree

import (
	"context"
	"testing"

	"github.com/astralp2p/astral-go/api/tree"
	"github.com/astralp2p/astral-go/astral"
	treemod "github.com/astralp2p/astrald/mod/tree"
)

// mountedNode is a node mounted over a stored path; id keeps instances distinct.
type mountedNode struct {
	treemod.NilNode
	id int
}

// Siblings below depth 3 each keep their own path, and a mount resolves under its own parent only.
func TestNodeWrapperSiblingPaths(t *testing.T) {
	ctx := astral.NewContext(context.Background())
	mod := newConfigureNodeStateTree(t, &recordingAuth{})

	names := []string{"x", "y", "z"}
	for _, name := range names {
		if _, err := tree.Query(ctx, mod.Root(), "/a/b/c/"+name+"/m", true); err != nil {
			t.Fatalf("create /a/b/c/%s/m: %v", name, err)
		}
	}

	mounted := &mountedNode{id: 1}
	if err := mod.Mount("/a/b/c/x/m", mounted); err != nil {
		t.Fatalf("mount: %v", err)
	}

	c, err := tree.Query(ctx, mod.Root(), "/a/b/c", false)
	if err != nil {
		t.Fatalf("query /a/b/c: %v", err)
	}

	sub, err := c.Sub(ctx)
	if err != nil {
		t.Fatalf("sub /a/b/c: %v", err)
	}

	for _, name := range names {
		child := sub[name].(*NodeWrapper)
		if got, want := child.Path(), "/a/b/c/"+name; got != want {
			t.Errorf("child %s Path() = %s, want %s", name, got, want)
		}

		grand, err := child.Sub(ctx)
		if err != nil {
			t.Fatalf("sub /a/b/c/%s: %v", name, err)
		}

		isMounted := grand["m"].(*NodeWrapper).Node == tree.Node(mounted)
		if isMounted != (name == "x") {
			t.Errorf("/a/b/c/%s/m resolved to the mount = %v, want %v", name, isMounted, name == "x")
		}
	}

	v, err := c.Create(ctx, "v")
	if err != nil {
		t.Fatalf("create /a/b/c/v: %v", err)
	}
	if _, err := c.Create(ctx, "w"); err != nil {
		t.Fatalf("create /a/b/c/w: %v", err)
	}
	if got := v.(*NodeWrapper).Path(); got != "/a/b/c/v" {
		t.Errorf("created child v Path() = %s, want /a/b/c/v", got)
	}
}
