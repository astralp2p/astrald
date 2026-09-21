package objects

import (
	"errors"
	"io"
	"sync"
	"testing"

	"github.com/astralp2p/astral-go/astral"
)

// aliasModeProto is a test stand-in for app-level types like nearby.Mode: a Uint8 newtype
// that satisfies PrimitiveAlias so GetBlueprint can derive its alias-kind Blueprint.
type aliasModeProto astral.Uint8

// astral:blueprint-ignore
func (*aliasModeProto) ObjectType() string          { return "test.objects.alias_mode" }
func (*aliasModeProto) UnderlyingPrimitive() string { return "uint8" }
func (m *aliasModeProto) WriteTo(w io.Writer) (int64, error) {
	return (*astral.Uint8)(m).WriteTo(w)
}
func (m *aliasModeProto) ReadFrom(r io.Reader) (int64, error) {
	return (*astral.Uint8)(m).ReadFrom(r)
}

// why: astral.Register and astral.Add write a process-wide registry that exposes
// no removal call, so a second -count iteration in the same process re-registers
// and fails. Each registration runs once per process; the error it returned is
// kept and re-checked every iteration, so a genuine registration failure still
// fails every run.
var (
	bpStructOnce sync.Once
	bpStructErr  error

	bpAliasOnce sync.Once
	bpAliasErr  error

	aliasModeOnce sync.Once
	aliasModeErr  error
)

func TestGetBlueprint_Primitive(t *testing.T) {
	var mod Module
	_, err := mod.GetBlueprint("uint8")
	if !errors.Is(err, astral.ErrPrimitiveType) {
		t.Fatalf("want ErrPrimitiveType, got %v", err)
	}
}

func TestGetBlueprint_NotFound(t *testing.T) {
	var mod Module
	_, err := mod.GetBlueprint("test.objects.nonexistent")
	if !errors.Is(err, astral.ErrBlueprintNotFound) {
		t.Fatalf("want ErrBlueprintNotFound, got %v", err)
	}
}

func TestGetBlueprint_RuntimeStruct(t *testing.T) {
	var mod Module
	bpStructOnce.Do(func() {
		_, bpStructErr = astral.Register(astral.NewBlueprint("test.objects.bp_struct",
			astral.Field{Name: "n", Spec: &astral.PrimitiveSpec{PrimitiveType: "uint32"}},
		))
	})
	if bpStructErr != nil {
		t.Fatal(bpStructErr)
	}

	got, err := mod.GetBlueprint("test.objects.bp_struct")
	if err != nil {
		t.Fatal(err)
	}
	if got.Kind() != astral.BlueprintKindStruct || len(got.Fields) != 1 {
		t.Fatalf("unexpected blueprint: %+v", got)
	}
}

func TestGetBlueprint_RuntimeAlias(t *testing.T) {
	var mod Module
	bpAliasOnce.Do(func() {
		_, bpAliasErr = astral.Register(astral.NewBlueprintAlias("test.objects.bp_alias", "uint8"))
	})
	if bpAliasErr != nil {
		t.Fatal(bpAliasErr)
	}

	got, err := mod.GetBlueprint("test.objects.bp_alias")
	if err != nil {
		t.Fatal(err)
	}
	if got.Kind() != astral.BlueprintKindAlias || got.Underlying.String() != "uint8" {
		t.Fatalf("unexpected blueprint: %+v", got)
	}
}

func TestGetBlueprint_DerivedAliasPrototype(t *testing.T) {
	var mod Module
	aliasModeOnce.Do(func() { aliasModeErr = astral.Add(new(aliasModeProto)) })
	if aliasModeErr != nil {
		t.Fatal(aliasModeErr)
	}

	got, err := mod.GetBlueprint("test.objects.alias_mode")
	if err != nil {
		t.Fatal(err)
	}
	if got.Kind() != astral.BlueprintKindAlias || got.Underlying.String() != "uint8" {
		t.Fatalf("unexpected blueprint: %+v", got)
	}
}

func TestGetBlueprint_DerivedStructPrototype(t *testing.T) {
	var mod Module
	// note: "astral.blueprint" is a compile-time prototype, never a runtime Blueprint,
	// so this exercises the BlueprintOf derivation path.
	got, err := mod.GetBlueprint("astral.blueprint")
	if err != nil {
		t.Fatal(err)
	}
	if got.Kind() != astral.BlueprintKindStruct || got.Type.String() != "astral.blueprint" {
		t.Fatalf("unexpected blueprint: %+v", got)
	}
}
