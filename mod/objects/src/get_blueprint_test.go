package objects

import (
	"errors"
	"io"
	"testing"

	"github.com/cryptopunkscc/astral-go/astral"
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
	bp := astral.NewBlueprint("test.objects.bp_struct",
		astral.Field{Name: "n", Spec: &astral.PrimitiveSpec{PrimitiveType: "uint32"}},
	)
	if _, err := astral.Register(bp); err != nil {
		t.Fatal(err)
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
	if _, err := astral.Register(astral.NewBlueprintAlias("test.objects.bp_alias", "uint8")); err != nil {
		t.Fatal(err)
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
	if err := astral.Add(new(aliasModeProto)); err != nil {
		t.Fatal(err)
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
