package views

import (
	"regexp"
	"strings"
	"testing"

	"github.com/astralp2p/astral-go/astral"
	"github.com/astralp2p/astral-go/astral/fmt"
)

// ansi strips lipgloss color escapes so assertions compare on rendered text alone.
var ansi = regexp.MustCompile(`\x1b\[[0-9;]*m`)

func render(o astral.Object) string {
	return ansi.ReplaceAllString(fmt.ViewFor(o).Render(), "")
}

func mustRO(t *testing.T, bp *astral.Blueprint) *astral.RuntimeObject {
	t.Helper()
	ro, err := astral.NewRuntimeObject(bp)
	if err != nil {
		t.Fatal(err)
	}
	return ro
}

func TestRuntimeObjectView_Struct(t *testing.T) {
	bp := astral.NewBlueprint("test.song",
		astral.Field{Name: "title", Spec: &astral.PrimitiveSpec{PrimitiveType: "string16"}},
		astral.Field{Name: "track", Spec: &astral.PrimitiveSpec{PrimitiveType: "uint32"}},
	)
	ro := mustRO(t, bp)
	if err := ro.Set("title", "My Song"); err != nil {
		t.Fatal(err)
	}
	if err := ro.Set("track", uint32(7)); err != nil {
		t.Fatal(err)
	}

	got := render(ro)

	// why: a raw go dump leaks pointer hex and the &{ struct prefix; the generic view must not.
	if strings.Contains(got, "0x") || strings.HasPrefix(got, "&{") {
		t.Fatalf("render leaked a go dump: %q", got)
	}
	for _, want := range []string{"test.song", "title: ", "My Song", "track: ", "7"} {
		if !strings.Contains(got, want) {
			t.Fatalf("render %q missing %q", got, want)
		}
	}
	// declared field order is preserved
	if strings.Index(got, "title") > strings.Index(got, "track") {
		t.Fatalf("fields out of declared order: %q", got)
	}
}

func TestRuntimeObjectView_Alias(t *testing.T) {
	bp := astral.NewBlueprintAlias("test.level", "uint8")
	ro := mustRO(t, bp)
	if err := ro.UnmarshalJSON([]byte("9")); err != nil {
		t.Fatal(err)
	}

	got := render(ro)

	if !strings.Contains(got, "test.level") || !strings.Contains(got, "9") {
		t.Fatalf("alias render %q missing type or value", got)
	}
	if !strings.Contains(got, "(") || !strings.Contains(got, ")") {
		t.Fatalf("alias render %q not in Type(value) form", got)
	}
}

func TestRuntimeSliceView(t *testing.T) {
	rs, err := astral.NewRuntimeSlice("uint32")
	if err != nil {
		t.Fatal(err)
	}
	for _, v := range []uint32{1, 2, 3} {
		if err := rs.Append(astral.NewUint32(v)); err != nil {
			t.Fatal(err)
		}
	}

	got := render(rs)

	if strings.Contains(got, "0x") {
		t.Fatalf("slice render leaked a go dump: %q", got)
	}
	for _, want := range []string{"[", "1", "2", "3", "]"} {
		if !strings.Contains(got, want) {
			t.Fatalf("slice render %q missing %q", got, want)
		}
	}
}
