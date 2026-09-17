package core

import (
	"errors"
	"testing"

	"github.com/astralp2p/astral-go/astral"
	"github.com/astralp2p/astral-go/astral/log"
	"github.com/astralp2p/astrald/core/assets"
)

type modA struct{}

func (*modA) Run(*astral.Context) error { return nil }

type modB struct{}

func (*modB) Run(*astral.Context) error { return nil }

type loaderFunc func(astral.Node, assets.Assets, *log.Logger) (Module, error)

func (f loaderFunc) Load(node astral.Node, a assets.Assets, l *log.Logger) (Module, error) {
	return f(node, a, l)
}

func newModulesNode() (*Node, *modA, *modB) {
	a, b := &modA{}, &modB{}
	node := &Node{modules: &Modules{loaded: map[string]Module{"a": a, "custom": b}}}
	return node, a, b
}

func TestInjectSetsExportedFields(t *testing.T) {
	node, a, b := newModulesNode()
	var deps struct {
		A      *modA
		B      *modB `mod:"custom"`
		hidden *modA
	}

	if err := Inject(node, &deps); err != nil {
		t.Fatalf("Inject: %v", err)
	}
	if deps.A != a {
		t.Errorf("A = %p, want module a %p", deps.A, a)
	}
	if deps.B != b {
		t.Errorf("B = %p, want module custom %p", deps.B, b)
	}
	if deps.hidden != nil {
		t.Errorf("unexported hidden = %p, want nil", deps.hidden)
	}
}

func TestInjectErrors(t *testing.T) {
	node, _, _ := newModulesNode()

	var wrongType struct{ A *modB }
	var missing struct{ Zzz Module }
	var value struct{ A *modA }

	tests := []struct {
		name   string
		node   astral.Node
		target any
		want   string
	}{
		{name: "wrong field type", node: node, target: &wrongType, want: "cannot inject field A"},
		{name: "missing module", node: node, target: &missing, want: "cannot find module zzz"},
		{name: "struct value", node: node, target: value, want: "expected a pointer to a struct"},
		{name: "nil node", node: nil, target: &value, want: "unsupported node type"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := Inject(tt.node, tt.target)
			if err == nil || err.Error() != tt.want {
				t.Fatalf("Inject error = %v, want %q", err, tt.want)
			}
		})
	}
}

func TestLoad(t *testing.T) {
	node, a, _ := newModulesNode()

	got, err := Load[*modA](node, "a")
	if err != nil || got != a {
		t.Fatalf("Load[*modA](a) = (%p, %v), want (%p, nil)", got, err, a)
	}

	_, err = Load[*modB](node, "a")
	var unavailable ErrModuleUnavailable
	if !errors.As(err, &unavailable) || unavailable.Name != "a" {
		t.Fatalf("Load[*modB](a) error = %#v, want ErrModuleUnavailable{Name: a}", err)
	}
	if want := "module a unavailable"; err.Error() != want {
		t.Fatalf("Load[*modB](a) error = %q, want %q", err, want)
	}

	_, err = Load[*modB](nil, "a")
	if want := "unsupported node type"; err == nil || err.Error() != want {
		t.Fatalf("Load[*modB](nil node) error = %v, want %q", err, want)
	}
}

func TestEachLoadedModuleStopsOnError(t *testing.T) {
	node, _, _ := newModulesNode()
	errX := errors.New("x")

	calls := 0
	err := EachLoadedModule(node, func(Module) error {
		calls++
		return errX
	})

	if !errors.Is(err, errX) {
		t.Fatalf("EachLoadedModule error = %v, want %v", err, errX)
	}
	if calls != 1 {
		t.Fatalf("fn called %d times, want 1", calls)
	}
}

func TestRegisterModuleRejectsDuplicate(t *testing.T) {
	const name = "core-test-register-module"
	loader := loaderFunc(func(astral.Node, assets.Assets, *log.Logger) (Module, error) {
		return &modA{}, nil
	})
	if _, found := modules.Get(name); found {
		t.Fatalf("module %q is already registered", name)
	}
	t.Cleanup(func() { modules.Delete(name) })

	if err := RegisterModule(name, loader); err != nil {
		t.Fatalf("first RegisterModule: %v", err)
	}
	err := RegisterModule(name, loader)
	if want := "module already added"; err == nil || err.Error() != want {
		t.Fatalf("second RegisterModule error = %v, want %q", err, want)
	}
}
