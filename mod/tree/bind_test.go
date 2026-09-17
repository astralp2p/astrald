package tree

import (
	"errors"
	"testing"

	"github.com/astralp2p/astral-go/api/tree"
	"github.com/astralp2p/astral-go/astral"
)

// note: any tree.Node method other than Sub and Create panics on the nil embedded interface.
type mapNode struct {
	tree.Node
	path string
	subs map[string]*mapNode
}

func newMapNode(path string) *mapNode {
	return &mapNode{path: path, subs: map[string]*mapNode{}}
}

func (n *mapNode) Sub(*astral.Context) (map[string]tree.Node, error) {
	sub := make(map[string]tree.Node, len(n.subs))
	for name, node := range n.subs {
		sub[name] = node
	}
	return sub, nil
}

func (n *mapNode) Create(_ *astral.Context, name string) (tree.Node, error) {
	path := n.path + "/" + name
	if n.path == "/" {
		path = "/" + name
	}

	child := newMapNode(path)
	n.subs[name] = child
	return child, nil
}

type boundValue struct {
	node tree.Node
}

func (v *boundValue) Bind(_ *astral.Context, node tree.Node) error {
	v.node = node
	return nil
}

func (v *boundValue) path() string {
	if n, ok := v.node.(*mapNode); ok {
		return n.path
	}
	return ""
}

type failingValue struct{}

func (*failingValue) Bind(*astral.Context, tree.Node) error {
	return errors.New("boom")
}

func TestParseTag(t *testing.T) {
	tests := []struct {
		in   string
		want tag
	}{
		{"", tag{path: "", skip: false}},
		{"a", tag{path: "a", skip: false}},
		{"a;skip", tag{path: "a", skip: true}},
		{";skip", tag{path: "", skip: true}},
	}

	for _, tt := range tests {
		if got := parseTag(tt.in); got != tt.want {
			t.Errorf("parseTag(%q) = %+v; want %+v", tt.in, got, tt.want)
		}
	}
}

func TestBindRejectsNonStructPointer(t *testing.T) {
	ctx := astral.NewContext(nil)
	const want = "s must be a pointer to a struct"

	var intVar int
	targets := map[string]any{
		"struct value": struct{ Foo boundValue }{},
		"int pointer":  &intVar,
	}

	for name, target := range targets {
		if err := Bind(ctx, target, newMapNode("/")); err == nil || err.Error() != want {
			t.Errorf("Bind(%s) err = %v; want %q", name, err, want)
		}
	}
}

func TestBindFieldPaths(t *testing.T) {
	var s struct {
		Foo      boundValue
		BarBaz   *boundValue
		hidden   boundValue
		Renamed  boundValue `tree:"custom"`
		Nested   struct{ Inner boundValue }
		HTTPPort boundValue
	}

	if err := Bind(astral.NewContext(nil), &s, newMapNode("/")); err != nil {
		t.Fatalf("Bind err = %v; want nil", err)
	}

	if s.BarBaz == nil {
		t.Fatal("Bind left the nil BarBaz pointer unallocated")
	}

	tests := []struct {
		field string
		got   string
		want  string
	}{
		{"Foo", s.Foo.path(), "/foo"},
		{"BarBaz", s.BarBaz.path(), "/bar_baz"},
		{"Renamed", s.Renamed.path(), "/custom"},
		{"Nested.Inner", s.Nested.Inner.path(), "/nested/inner"},
		{"HTTPPort", s.HTTPPort.path(), "/http_port"},
	}

	for _, tt := range tests {
		if tt.got != tt.want {
			t.Errorf("field %s bound at %q; want %q", tt.field, tt.got, tt.want)
		}
	}

	if s.hidden.node != nil {
		t.Errorf("unexported field hidden was bound to %v; want untouched", s.hidden.node)
	}
}

func TestBindWrapsFieldError(t *testing.T) {
	var s struct {
		Bad failingValue
	}

	err := Bind(astral.NewContext(nil), &s, newMapNode("/"))

	const want = "failed to bind field Bad to key bad: boom"
	if err == nil || err.Error() != want {
		t.Fatalf("Bind err = %v; want %q", err, want)
	}
}

func TestBindPathCreatesPrefix(t *testing.T) {
	var s struct {
		Foo boundValue
	}

	if err := BindPath(astral.NewContext(nil), &s, newMapNode("/"), "/mod/x/config", true); err != nil {
		t.Fatalf("BindPath err = %v; want nil", err)
	}

	if got, want := s.Foo.path(), "/mod/x/config/foo"; got != want {
		t.Fatalf("Foo bound at %q; want %q", got, want)
	}
}
