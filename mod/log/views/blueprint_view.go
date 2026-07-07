package views

import (
	"strconv"

	"github.com/cryptopunkscc/astral-go/astral"
	"github.com/cryptopunkscc/astral-go/astral/fmt"
	"github.com/cryptopunkscc/astral-go/astral/log/styles"
	"github.com/cryptopunkscc/astral-go/astral/log/theme"
)

type BlueprintView struct {
	*astral.Blueprint
}

func (v BlueprintView) Render() (out string) {
	out += theme.Type.Bri(theme.Less).Render(v.Type.String())

	// alias kind: name = underlying
	if v.Kind() == astral.BlueprintKindAlias {
		out += styles.Highlight.Render(" = ")
		out += theme.Type.Render(v.Underlying.String())
		return
	}

	// struct kind: name{Field spec, Field spec}
	out += styles.Highlight.Render("{")

	var first = true
	for _, f := range v.Fields {
		if !first {
			out += ", "
		}
		out += theme.Normal.Render(f.Name.String()) + " " +
			renderSpec(f.Spec)
		first = false
	}

	out += styles.Highlight.Render("}")

	return
}

// renderSpec renders a Spec carrier in Go-like syntax; heterogeneous slots render as "any".
// Structural tokens ([], [N], map[], *) use the highlight color, type names the type color.
func renderSpec(s astral.Spec) string {
	switch v := s.(type) {
	case *astral.PrimitiveSpec:
		return theme.Type.Render(v.PrimitiveType.String())
	case *astral.RefSpec:
		return theme.Type.Render(v.Type.String())
	case *astral.SliceSpec:
		return styles.Highlight.Render("[]") +
			theme.Type.Render(orAny(v.Type.String()))
	case *astral.ArraySpec:
		return styles.Highlight.Render("["+strconv.Itoa(int(v.Length))+"]") +
			theme.Type.Render(orAny(v.Type.String()))
	case *astral.MapSpec:
		return styles.Highlight.Render("map[") +
			theme.Type.Render(v.KeyType.String()) +
			styles.Highlight.Render("]") +
			theme.Type.Render(orAny(v.ValueType.String()))
	case *astral.PtrSpec:
		return styles.Highlight.Render("*") +
			theme.Type.Render(v.Type.String())
	case *astral.ObjectSpec:
		return theme.Type.Render("any")
	}
	return theme.Type.Render("?")
}

func orAny(s string) string {
	if s == "" {
		return "any"
	}
	return s
}

func init() {
	fmt.SetView(func(o *astral.Blueprint) fmt.View {
		return &BlueprintView{Blueprint: o}
	})
}
