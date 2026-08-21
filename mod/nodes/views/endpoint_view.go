package nodes

import (
	"github.com/astralp2p/astral-go/api/nodes"
	"github.com/astralp2p/astral-go/astral/fmt"
	"github.com/astralp2p/astral-go/astral/log/theme"
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
