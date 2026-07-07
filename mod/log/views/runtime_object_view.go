package views

import (
	"github.com/cryptopunkscc/astral-go/astral"
	"github.com/cryptopunkscc/astral-go/astral/fmt"
	"github.com/cryptopunkscc/astral-go/astral/log/styles"
	"github.com/cryptopunkscc/astral-go/astral/log/theme"
)

// RuntimeObjectView renders a blueprint-backed object whose type has no compiled-in view. It is
// the generic fallback for external types: struct kind renders Type{Field: value, ...}, alias
// kind renders Type(value). Each field value delegates to fmt.ViewFor, so nested primitives and
// nested external types render through the same machinery. See issue #337.
type RuntimeObjectView struct {
	*astral.RuntimeObject
}

func (v RuntimeObjectView) Render() (out string) {
	bp := v.Blueprint()
	out += theme.Type.Bri(theme.Less).Render(v.ObjectType())

	// alias kind: Type(value)
	if bp.Kind() == astral.BlueprintKindAlias {
		out += styles.Highlight.Render("(") +
			fmt.ViewFor(v.Underlying()).Render() +
			styles.Highlight.Render(")")
		return
	}

	// struct kind: Type{Field: value, Field: value}
	out += styles.Highlight.Render("{")
	for i, f := range bp.Fields {
		if i > 0 {
			out += ", "
		}
		name := f.Name.String()
		out += theme.Normal.Render(name) + ": " + fmt.ViewFor(v.Get(name)).Render()
	}
	out += styles.Highlight.Render("}")

	return
}

func init() {
	// why: external type names are runtime-only, so register as the fmt fallback rather than a
	// per-type builder. Decline non-RuntimeObject and unbound carriers — they fall to Stringify.
	fmt.SetFallbackView(func(o astral.Object) fmt.View {
		ro, ok := o.(*astral.RuntimeObject)
		if !ok || ro.Blueprint() == nil {
			return nil
		}
		return RuntimeObjectView{RuntimeObject: ro}
	})
}
