package nodes

import (
	"github.com/cryptopunkscc/astral-go/astral/fmt"
	"github.com/cryptopunkscc/astral-go/astral/log/theme"
	"github.com/cryptopunkscc/astrald/mod/nodes"
)

type EndpointView struct {
	*nodes.EndpointWithTTL
}

func (v *EndpointView) Render() string {
	n := theme.Tertiary
	b := n.Bri(theme.More)

	return n.Render(v.Network()+":") + b.Render(v.Address())
}

func init() {
	fmt.SetView(func(o *nodes.EndpointWithTTL) fmt.View {
		return &EndpointView{EndpointWithTTL: o}
	})
}
