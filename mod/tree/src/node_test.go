package tree

import (
	"errors"
	"testing"
	"time"

	"github.com/astralp2p/astral-go/api/tree"
	"github.com/astralp2p/astral-go/astral"
)

func TestNodeRootCannotHoldValue(t *testing.T) {
	mod := newConfigureNodeStateTree(t, &recordingAuth{})
	root := &Node{mod: mod}
	ctx := astral.NewContext(nil)
	const want = "root node cannot hold a value"

	if _, err := root.Get(ctx, false); err == nil || err.Error() != want {
		t.Fatalf("root Get err = %v; want %q", err, want)
	}

	if err := root.Set(ctx, astral.NewString8("v")); err == nil || err.Error() != want {
		t.Fatalf("root Set err = %v; want %q", err, want)
	}
}

func TestNodeCreate(t *testing.T) {
	mod := newConfigureNodeStateTree(t, &recordingAuth{})
	root := &Node{mod: mod}
	ctx := astral.NewContext(nil)

	if _, err := root.Create(ctx, ""); err == nil || err.Error() != "name cannot be empty" {
		t.Fatalf("Create(\"\") err = %v; want %q", err, "name cannot be empty")
	}

	created, err := root.Create(ctx, "k")
	if err != nil {
		t.Fatalf("Create(k) err = %v; want nil", err)
	}

	if node, ok := created.(*Node); !ok || node.Name() != "k" {
		t.Fatalf("Create(k) = %#v; want *Node named k", created)
	}

	if _, err := root.Create(ctx, "k"); !errors.Is(err, tree.ErrAlreadyExists) {
		t.Fatalf("second Create(k) err = %v; want %v", err, tree.ErrAlreadyExists)
	}
}

func TestNodeGetSetValue(t *testing.T) {
	mod := newConfigureNodeStateTree(t, &recordingAuth{})
	ctx := astral.NewContext(nil)
	node := createTestNode(t, mod, "k")

	if got := readValues(t, node, ctx); len(got) != 1 {
		t.Fatalf("new node yielded %d values; want 1", len(got))
	} else if _, ok := got[0].(*astral.Nil); !ok {
		t.Fatalf("new node value = %T; want *astral.Nil", got[0])
	}

	if err := node.Set(ctx, astral.NewString8("v")); err != nil {
		t.Fatalf("Set(v) err = %v", err)
	}

	got := readValues(t, node, ctx)
	if len(got) != 1 {
		t.Fatalf("Get after Set(v) yielded %d values; want 1", len(got))
	}
	if s, ok := got[0].(*astral.String8); !ok || string(*s) != "v" {
		t.Fatalf("Get after Set(v) = %v; want string8 v", got[0])
	}

	if err := node.Set(ctx, nil); err != nil {
		t.Fatalf("Set(nil) err = %v", err)
	}

	got = readValues(t, node, ctx)
	if len(got) != 1 {
		t.Fatalf("Get after Set(nil) yielded %d values; want 1", len(got))
	}
	if _, ok := got[0].(*astral.Nil); !ok {
		t.Fatalf("Get after Set(nil) = %T; want *astral.Nil", got[0])
	}
}

func TestNodeDeleteRefusesNodeWithSubnodes(t *testing.T) {
	mod := newConfigureNodeStateTree(t, &recordingAuth{})
	ctx := astral.NewContext(nil)
	root := &Node{mod: mod}
	parent := createTestNode(t, mod, "k")

	child, err := parent.Create(ctx, "sub")
	if err != nil {
		t.Fatalf("create /k/sub: %v", err)
	}

	if err := parent.Delete(ctx); !errors.Is(err, tree.ErrNodeHasSubnodes) {
		t.Fatalf("Delete /k with a child err = %v; want %v", err, tree.ErrNodeHasSubnodes)
	}

	if err := child.Delete(ctx); err != nil {
		t.Fatalf("Delete /k/sub err = %v; want nil", err)
	}

	if err := parent.Delete(ctx); err != nil {
		t.Fatalf("Delete /k after its child err = %v; want nil", err)
	}

	sub, err := root.Sub(ctx)
	if err != nil {
		t.Fatalf("root Sub: %v", err)
	}

	if _, found := sub["k"]; found {
		t.Fatalf("root Sub lists k after delete: %v", sub)
	}
}

func TestDBGetNodeValueUnregisteredType(t *testing.T) {
	mod := newConfigureNodeStateTree(t, &recordingAuth{})
	node := createTestNode(t, mod, "k")

	err := mod.db.Model(&dbNode{}).Where("id = ?", node.id).
		Updates(map[string]any{"type": "unregistered.type", "payload": []byte{1, 2}}).Error
	if err != nil {
		t.Fatalf("store unregistered type: %v", err)
	}

	obj, err := mod.db.getNodeValue(node.id, true)
	if err != nil {
		t.Fatalf("getNodeValue(allowUnparsed=true) err = %v; want nil", err)
	}
	if _, ok := obj.(*astral.UnparsedObject); !ok {
		t.Fatalf("getNodeValue(allowUnparsed=true) = %T; want *astral.UnparsedObject", obj)
	}

	if _, err := mod.db.getNodeValue(node.id, false); !errors.Is(err, astral.ErrBlueprintNotFound) {
		t.Fatalf("getNodeValue(allowUnparsed=false) err = %v; want %v", err, astral.ErrBlueprintNotFound)
	}
}

func TestNodeGetFollow(t *testing.T) {
	mod := newConfigureNodeStateTree(t, &recordingAuth{})
	node := createTestNode(t, mod, "k")

	if err := node.Set(astral.NewContext(nil), astral.NewString8("v1")); err != nil {
		t.Fatalf("Set(v1): %v", err)
	}

	ctx, cancel := astral.NewContext(nil).WithCancel()
	defer cancel()

	values, err := node.Get(ctx, true)
	if err != nil {
		t.Fatalf("Get(follow=true) err = %v", err)
	}

	expectString8(t, values, "v1")

	if err := node.Set(astral.NewContext(nil), astral.NewString8("v2")); err != nil {
		t.Fatalf("Set(v2): %v", err)
	}

	expectString8(t, values, "v2")

	cancel()

	deadline := time.After(2 * time.Second)
	for {
		select {
		case _, ok := <-values:
			if !ok {
				return
			}
		case <-deadline:
			t.Fatal("follow channel stayed open after the context was canceled")
		}
	}
}

func createTestNode(t *testing.T, mod *Module, name string) *Node {
	t.Helper()

	created, err := (&Node{mod: mod}).Create(astral.NewContext(nil), name)
	if err != nil {
		t.Fatalf("create /%s: %v", name, err)
	}

	return created.(*Node)
}

func readValues(t *testing.T, node *Node, ctx *astral.Context) []astral.Object {
	t.Helper()

	ch, err := node.Get(ctx, false)
	if err != nil {
		t.Fatalf("Get(follow=false) err = %v", err)
	}

	var values []astral.Object
	deadline := time.After(2 * time.Second)
	for {
		select {
		case v, ok := <-ch:
			if !ok {
				return values
			}
			values = append(values, v)
		case <-deadline:
			t.Fatal("Get(follow=false) channel did not close")
		}
	}
}

func expectString8(t *testing.T, ch <-chan astral.Object, want string) {
	t.Helper()

	select {
	case v, ok := <-ch:
		if !ok {
			t.Fatalf("follow channel closed; want string8 %s", want)
		}
		if s, isString := v.(*astral.String8); !isString || string(*s) != want {
			t.Fatalf("follow channel yielded %v; want string8 %s", v, want)
		}
	case <-time.After(2 * time.Second):
		t.Fatalf("follow channel yielded nothing; want string8 %s", want)
	}
}
