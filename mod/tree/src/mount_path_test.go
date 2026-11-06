package tree

import (
	"bytes"
	"context"
	"errors"
	"slices"
	"testing"
	"time"

	"github.com/astralp2p/astral-go/api/tree"
	"github.com/astralp2p/astral-go/astral"
	"github.com/astralp2p/astral-go/astral/channel"
	"github.com/astralp2p/astral-go/sig"
	treemod "github.com/astralp2p/astrald/mod/tree"
)

// statusNode is a mounted node that computes its value.
type statusNode struct {
	treemod.NilNode
}

func (statusNode) Get(*astral.Context, bool) (<-chan astral.Object, error) {
	status := astral.String8("running")
	return sig.ArrayToChan([]astral.Object{&status}), nil
}

// newMountedTree returns an empty tree with statusNode mounted at /mod/myapp/status.
func newMountedTree(t *testing.T) (*Module, *astral.Context, tree.Node) {
	t.Helper()

	mod := newConfigureNodeStateTree(t, &recordingAuth{verdict: true})
	mounted := &statusNode{}
	if err := mod.Mount("/mod/myapp/status", mounted); err != nil {
		t.Fatalf("Mount(/mod/myapp/status): %v", err)
	}

	return mod, astral.NewContext(context.Background()), mounted
}

// countRows returns the number of stored tree nodes.
func countRows(t *testing.T, mod *Module) int64 {
	t.Helper()

	var n int64
	if err := mod.db.Model(&dbNode{}).Count(&n).Error; err != nil {
		t.Fatalf("count rows: %v", err)
	}
	return n
}

// A mount is reachable at a path no stored node holds, by a read and by a create walk alike.
func TestMountReachableWithoutStoredPath(t *testing.T) {
	mod, ctx, mounted := newMountedTree(t)

	for _, create := range []bool{false, true} {
		node, err := tree.Query(ctx, mod.Root(), "/mod/myapp/status", create)
		if err != nil {
			t.Fatalf("Query(create=%v): %v", create, err)
		}
		if got := node.(*NodeWrapper).Node; got != mounted {
			t.Errorf("Query(create=%v) reached %v; want the mount", create, got)
		}
		if got := node.(*NodeWrapper).Path(); got != "/mod/myapp/status" {
			t.Errorf("Query(create=%v) Path() = %s; want /mod/myapp/status", create, got)
		}
	}

	got, err := mod.Get(ctx, "/mod/myapp/status")
	if err != nil {
		t.Fatalf("Get(/mod/myapp/status): %v", err)
	}
	if s, ok := got.(*astral.String8); !ok || string(*s) != "running" {
		t.Errorf("Get(/mod/myapp/status) = %v; want running", got)
	}

	if n := countRows(t, mod); n != 0 {
		t.Errorf("reaching the mount stored %d nodes; want 0", n)
	}
}

// Every segment on the way to a mount is listed next to the stored subnodes.
func TestMountPathListed(t *testing.T) {
	mod, ctx, _ := newMountedTree(t)

	listen := astral.String8("tcp:0.0.0.0:1791")
	if err := mod.Set(ctx, "/mod/tcp/listen", &listen); err != nil {
		t.Fatalf("Set(/mod/tcp/listen): %v", err)
	}

	for path, want := range map[string][]string{
		"/":          {"mod"},
		"/mod":       {"myapp", "tcp"},
		"/mod/myapp": {"status"},
	} {
		node, err := tree.Query(ctx, mod.Root(), path, false)
		if err != nil {
			t.Fatalf("Query(%s): %v", path, err)
		}
		sub, err := node.Sub(ctx)
		if err != nil {
			t.Fatalf("Sub(%s): %v", path, err)
		}
		if got := sortedKeys(sub); !slices.Equal(got, want) {
			t.Errorf("Sub(%s) = %q; want %q", path, got, want)
		}
	}
}

// A write next to a mount stores the segment it passes through; the mount stays reachable.
func TestMountPathWriteStoresSegment(t *testing.T) {
	mod, ctx, mounted := newMountedTree(t)

	value := astral.String8("v")
	if err := mod.Set(ctx, "/mod/myapp/other", &value); err != nil {
		t.Fatalf("Set(/mod/myapp/other): %v", err)
	}
	if err := mod.Set(ctx, "/mod/myapp", &value); err != nil {
		t.Fatalf("Set(/mod/myapp): %v", err)
	}

	for _, path := range []string{"/mod/myapp/other", "/mod/myapp"} {
		got, err := mod.Get(ctx, path)
		if err != nil {
			t.Fatalf("Get(%s): %v", path, err)
		}
		if s, ok := got.(*astral.String8); !ok || *s != value {
			t.Errorf("Get(%s) = %v; want %v", path, got, value)
		}
	}

	node, err := tree.Query(ctx, mod.Root(), "/mod/myapp/status", false)
	if err != nil {
		t.Fatalf("Query(/mod/myapp/status): %v", err)
	}
	if node.(*NodeWrapper).Node != mounted {
		t.Error("/mod/myapp/status no longer reaches the mount after a write next to it")
	}

	if n := countRows(t, mod); n != 3 {
		t.Errorf("stored %d nodes; want 3 (mod, myapp, other)", n)
	}
}

// A segment held only by a mount reads nil, refuses deletion and stores nothing.
func TestMountPathReadAndDelete(t *testing.T) {
	mod, ctx, _ := newMountedTree(t)

	got, err := mod.Get(ctx, "/mod/myapp")
	if err != nil {
		t.Fatalf("Get(/mod/myapp): %v", err)
	}
	if _, ok := got.(*astral.Nil); !ok {
		t.Errorf("Get(/mod/myapp) = %v; want nil", got)
	}

	if err := mod.Delete(ctx, "/mod/myapp"); !errors.Is(err, tree.ErrNodeHasSubnodes) {
		t.Errorf("Delete(/mod/myapp) err = %v; want %v", err, tree.ErrNodeHasSubnodes)
	}

	if n := countRows(t, mod); n != 0 {
		t.Errorf("read and delete stored %d nodes; want 0", n)
	}
}

// A follow on a segment held only by a mount yields nil and stays open until the caller cancels.
func TestMountPathFollowStaysOpen(t *testing.T) {
	mod, ctx, _ := newMountedTree(t)

	node, err := tree.Query(ctx, mod.Root(), "/mod/myapp", false)
	if err != nil {
		t.Fatalf("Query(/mod/myapp): %v", err)
	}

	followCtx, cancel := ctx.WithCancel()
	defer cancel()
	ch, err := node.Get(followCtx, true)
	if err != nil {
		t.Fatalf("Get(/mod/myapp, follow): %v", err)
	}

	first, ok := <-ch
	if !ok {
		t.Fatal("Get(/mod/myapp, follow) closed before yielding a value")
	}
	if _, isNil := first.(*astral.Nil); !isNil {
		t.Errorf("Get(/mod/myapp, follow) yielded %v; want nil", first)
	}

	select {
	case object, ok := <-ch:
		t.Fatalf("follow did not stay open: got %v, open=%v", object, ok)
	case <-time.After(200 * time.Millisecond):
	}

	cancel()
	select {
	case _, ok := <-ch:
		if ok {
			t.Error("follow yielded a value after cancel; want close")
		}
	case <-time.After(10 * time.Second):
		t.Fatal("follow did not close after cancel")
	}

	if n := countRows(t, mod); n != 0 {
		t.Errorf("follow stored %d nodes; want 0", n)
	}
}

// A recursive delete over a mount fails, and the mount stays reachable.
func TestMountPathRecursiveDeleteRefused(t *testing.T) {
	mod, ctx, mounted := newMountedTree(t)
	caller := astral.GenerateIdentity()

	got := answers(t, mod.OpDelete, caller, "tree.delete?path=/mod/myapp&recursive=true")
	if len(got) != 1 {
		t.Fatalf("tree.delete answered %v; want one error", got)
	}
	if e, ok := got[0].(*astral.ErrorMessage); !ok || e.Error() != tree.ErrUnsupported.Error() {
		t.Errorf("tree.delete answered %v; want %v", got[0], tree.ErrUnsupported)
	}

	node, err := tree.Query(ctx, mod.Root(), "/mod/myapp/status", false)
	if err != nil {
		t.Fatalf("Query(/mod/myapp/status) after delete: %v", err)
	}
	if node.(*NodeWrapper).Node != mounted {
		t.Error("/mod/myapp/status no longer reaches the mount after a refused delete")
	}
}

// Unmount removes the segments the mount alone held.
func TestUnmountRemovesMountPath(t *testing.T) {
	mod, ctx, _ := newMountedTree(t)

	if err := mod.Unmount("/mod/myapp/status"); err != nil {
		t.Fatalf("Unmount: %v", err)
	}

	for _, path := range []string{"/mod", "/mod/myapp", "/mod/myapp/status"} {
		if _, err := tree.Query(ctx, mod.Root(), path, false); err == nil {
			t.Errorf("Query(%s) after Unmount succeeded; want not found", path)
		}
	}
}

// Create refuses a name a mount serves, so a stored node never hides the mount.
func TestNodeWrapperCreateRefusesMountedName(t *testing.T) {
	mod, ctx, _ := newMountedTree(t)

	myapp, err := tree.Query(ctx, mod.Root(), "/mod/myapp", false)
	if err != nil {
		t.Fatalf("Query(/mod/myapp): %v", err)
	}

	if _, err := myapp.Create(ctx, "status"); !errors.Is(err, tree.ErrAlreadyExists) {
		t.Errorf("Create(status) err = %v; want %v", err, tree.ErrAlreadyExists)
	}
}

// tree.list and tree.get answer a mount path no stored node holds.
func TestMountPathOverOps(t *testing.T) {
	mod, _, _ := newMountedTree(t)
	caller := astral.GenerateIdentity()

	list := answers(t, mod.OpList, caller, "tree.list?path=/mod/myapp")
	if len(list) != 2 || list[0].(*astral.String8).String() != "status" {
		t.Errorf("tree.list /mod/myapp answered %v; want status, eos", list)
	}

	get := answers(t, mod.OpGet, caller, "tree.get?path=/mod/myapp/status")
	if len(get) != 2 || get[0].(*astral.String8).String() != "running" {
		t.Errorf("tree.get /mod/myapp/status answered %v; want running, eos", get)
	}
}

// answers routes one query to op and returns every object it answered.
func answers(t *testing.T, op any, caller *astral.Identity, query string) (objects []astral.Object) {
	t.Helper()

	w := newRecordingWriter()
	if err := route(t, op, caller, query, w); err != nil {
		t.Fatalf("%s: %v", query, err)
	}
	select {
	case <-w.closed:
	case <-time.After(10 * time.Second):
		t.Fatalf("%s never closed the connection", query)
	}

	w.mu.Lock()
	defer w.mu.Unlock()
	receiver := channel.NewReceiver(bytes.NewReader(w.buf.Bytes()))
	for {
		object, err := receiver.Receive()
		if err != nil {
			return objects
		}
		objects = append(objects, object)
	}
}

func sortedKeys(m map[string]tree.Node) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	slices.Sort(keys)
	return keys
}
