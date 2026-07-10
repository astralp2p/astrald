package views

import (
	"strconv"

	"github.com/astralp2p/astral-go/astral"
	"github.com/astralp2p/astral-go/astral/fmt"
	"github.com/astralp2p/astral-go/astral/log/styles"
)

// RuntimeArrayView renders a blueprint-backed fixed-length array as [N]{elem, elem, ...},
// delegating each element to fmt.ViewFor. The carrier's ObjectType is the constant "array", so
// a per-type builder matches it directly. See issue #337.
type RuntimeArrayView struct {
	*astral.RuntimeArray
}

func (v RuntimeArrayView) Render() (out string) {
	out += styles.Highlight.Render("["+strconv.Itoa(v.Len())+"]") +
		styles.Highlight.Render("{")
	for i := 0; i < v.Len(); i++ {
		if i > 0 {
			out += ", "
		}
		out += fmt.ViewFor(v.At(i)).Render()
	}
	out += styles.Highlight.Render("}")

	return
}

func init() {
	fmt.SetView(func(o *astral.RuntimeArray) fmt.View {
		return RuntimeArrayView{RuntimeArray: o}
	})
}
