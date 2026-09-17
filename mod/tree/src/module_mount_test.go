package tree

import (
	"testing"
)

func TestMountRejectsRelativePath(t *testing.T) {
	mod := &Module{}

	if err := mod.Mount("rel", &Node{id: 1}); err == nil || err.Error() != "path must be absolute" {
		t.Fatalf("Mount(rel) err = %v; want %q", err, "path must be absolute")
	}

	if err := mod.Unmount("rel"); err == nil || err.Error() != "path must be absolute" {
		t.Fatalf("Unmount(rel) err = %v; want %q", err, "path must be absolute")
	}
}

func TestMountLifecycle(t *testing.T) {
	mod := &Module{}
	n := &Node{id: 1}
	m := &Node{id: 2}

	if err := mod.Mount("/x/", n); err != nil {
		t.Fatalf("Mount(/x/) err = %v; want nil", err)
	}

	for _, p := range []string{"/x", "/x/"} {
		if got := mod.getMount(p); got != n {
			t.Fatalf("getMount(%q) = %v; want the mounted node %v", p, got, n)
		}
	}

	if err := mod.Mount("/x", m); err == nil || err.Error() != "mount point already exists" {
		t.Fatalf("second Mount(/x) err = %v; want %q", err, "mount point already exists")
	}

	if got := mod.getMount("/x"); got != n {
		t.Fatalf("getMount(/x) after refused mount = %v; want the first node %v", got, n)
	}

	if err := mod.Unmount("/x/"); err != nil {
		t.Fatalf("Unmount(/x/) err = %v; want nil", err)
	}

	if got := mod.getMount("/x"); got != nil {
		t.Fatalf("getMount(/x) after unmount = %v; want nil", got)
	}

	if err := mod.Unmount("/x"); err == nil || err.Error() != "mount point does not exist" {
		t.Fatalf("second Unmount(/x) err = %v; want %q", err, "mount point does not exist")
	}
}

func TestGetMountRelativePathIsNil(t *testing.T) {
	mod := &Module{}
	mod.mounts.Set("x", &Node{id: 1})

	if got := mod.getMount("x"); got != nil {
		t.Fatalf("getMount(x) = %v; want nil", got)
	}
}

func TestRootWrapsRootMount(t *testing.T) {
	mod := &Module{}

	if got := mod.Root(); got != nil {
		t.Fatalf("Root() with nothing mounted = %v; want nil", got)
	}

	n := &Node{id: 1}
	mod.mounts.Set("/", n)

	wrap, ok := mod.Root().(*NodeWrapper)
	if !ok {
		t.Fatalf("Root() = %T; want *NodeWrapper", mod.Root())
	}

	if wrap.Node != n {
		t.Fatalf("Root() wraps %v; want the node mounted at / %v", wrap.Node, n)
	}
}
