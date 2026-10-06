package tree

import (
	"context"
	"errors"
	"io"
	"testing"

	"github.com/astralp2p/astral-go/api/tree"
	"github.com/astralp2p/astral-go/astral"
)

// unencodableObject fails to encode, so the DB write rejects it.
type unencodableObject struct{}

func (unencodableObject) ObjectType() string { return "test.unencodable" }

func (unencodableObject) WriteTo(io.Writer) (int64, error) {
	return 0, errors.New("encode failed")
}

func (unencodableObject) ReadFrom(io.Reader) (int64, error) { return 0, nil }

// A failed Set reaches no follower; the next value a follower sees is the next persisted one.
func TestNodeSetFailureDoesNotNotify(t *testing.T) {
	ctx, cancel := astral.NewContext(context.Background()).WithCancel()
	defer cancel()
	mod := newConfigureNodeStateTree(t, &recordingAuth{})

	node, err := tree.Query(ctx, mod.Root(), "/k", true)
	if err != nil {
		t.Fatalf("create /k: %v", err)
	}
	v1, v2 := astral.String8("v1"), astral.String8("v2")
	if err := node.Set(ctx, &v1); err != nil {
		t.Fatalf("set v1: %v", err)
	}

	follow, err := node.Get(ctx, true)
	if err != nil {
		t.Fatalf("follow: %v", err)
	}
	if got := <-follow; got.(*astral.String8).String() != "v1" {
		t.Fatalf("initial value = %v, want v1", got)
	}

	if err := node.Set(ctx, unencodableObject{}); err == nil {
		t.Fatal("Set of an unencodable object returned nil")
	}
	if err := node.Set(ctx, &v2); err != nil {
		t.Fatalf("set v2: %v", err)
	}

	got := <-follow
	if s, ok := got.(*astral.String8); !ok || s.String() != "v2" {
		t.Fatalf("follower received %T %v after a failed Set, want v2", got, got)
	}
}
