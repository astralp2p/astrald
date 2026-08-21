package views

import (
	"github.com/astralp2p/astral-go/astral"
	"github.com/astralp2p/astral-go/astral/fmt"
	"github.com/astralp2p/astral-go/astral/log/styles"
)

// RuntimeSliceView renders a blueprint-backed slice as [elem, elem, ...], delegating each
// element to fmt.ViewFor. The carrier's ObjectType is the constant "slice", so a per-type
// builder matches it directly. See issue #337.
type RuntimeSliceView struct {
	*astral.RuntimeSlice
}

func (v RuntimeSliceView) Render() (out string) {
	out += styles.Highlight.Render("[")
	for i := 0; i < v.Len(); i++ {
		if i > 0 {
			out += ", "
		}
		out += fmt.ViewFor(v.At(i)).Render()
	}
	out += styles.Highlight.Render("]")

	return
}

func init() {
	fmt.SetView(func(o *astral.RuntimeSlice) fmt.View {
		return RuntimeSliceView{RuntimeSlice: o}
	})
}
