package views

import (
	"testing"

	"github.com/astralp2p/astral-go/astral"
)

func TestRenderSpec(t *testing.T) {
	for _, c := range []struct {
		name string
		spec astral.Spec
		want string
	}{
		{"primitive", &astral.PrimitiveSpec{PrimitiveType: "uint8"}, "uint8"},
		{"ref", &astral.RefSpec{Type: "mod.x"}, "mod.x"},
		{"heterogeneous slice", &astral.SliceSpec{}, "[]any"},
		{"typed slice", &astral.SliceSpec{Type: "string8"}, "[]string8"},
		{"array", &astral.ArraySpec{Type: "uint8", Length: 4}, "[4]uint8"},
		{"heterogeneous map", &astral.MapSpec{KeyType: "string8"}, "map[string8]any"},
		{"pointer", &astral.PtrSpec{Type: "identity"}, "*identity"},
		{"object", &astral.ObjectSpec{}, "any"},
		{"nil", nil, "?"},
	} {
		if got := ansi.ReplaceAllString(renderSpec(c.spec), ""); got != c.want {
			t.Errorf("%s: renderSpec() = %q, want %q", c.name, got, c.want)
		}
	}
}

func TestBlueprintViewRender(t *testing.T) {
	for _, c := range []struct {
		name string
		bp   *astral.Blueprint
		want string
	}{
		{
			"struct",
			astral.NewBlueprint("test.song",
				astral.Field{Name: "title", Spec: &astral.PrimitiveSpec{PrimitiveType: "string16"}},
				astral.Field{Name: "tags", Spec: &astral.SliceSpec{Type: "string8"}},
			),
			"test.song{title string16, tags []string8}",
		},
		{
			"alias",
			astral.NewBlueprintAlias("test.level", "uint8"),
			"test.level = uint8",
		},
	} {
		got := ansi.ReplaceAllString(BlueprintView{Blueprint: c.bp}.Render(), "")
		if got != c.want {
			t.Errorf("%s: Render() = %q, want %q", c.name, got, c.want)
		}
	}
}
