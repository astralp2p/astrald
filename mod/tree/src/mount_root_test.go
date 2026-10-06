package tree

import (
	"context"
	"slices"
	"testing"

	"github.com/astralp2p/astral-go/api/tree"
	"github.com/astralp2p/astral-go/astral"
)

// The root mount is immutable: Mount and Unmount of "/" fail and leave it in place; other paths still mount.
func TestMountRootRefused(t *testing.T) {
	ctx := astral.NewContext(context.Background())
	mod := newConfigureNodeStateTree(t, &recordingAuth{})

	root, _ := mod.mounts.Get("/")

	if err := mod.Mount("/", &mountedNode{id: 1}); err == nil {
		t.Error("Mount(/) returned nil, want an error")
	}
	if err := mod.Unmount("/"); err == nil {
		t.Error("Unmount(/) returned nil, want an error")
	}

	if got, _ := mod.mounts.Get("/"); got != root {
		t.Errorf("root mount = %v, want %v", got, root)
	}
	if keys := mod.mounts.Keys(); !slices.Equal(keys, []string{"/"}) {
		t.Errorf("mount keys = %q, want [\"/\"]", keys)
	}

	if _, err := tree.Query(ctx, mod.Root(), "/a", true); err != nil {
		t.Fatalf("create /a: %v", err)
	}
	if err := mod.Mount("/a", &mountedNode{id: 2}); err != nil {
		t.Fatalf("Mount(/a): %v", err)
	}
	if err := mod.Unmount("/a/"); err != nil {
		t.Fatalf("Unmount(/a/): %v", err)
	}
}
