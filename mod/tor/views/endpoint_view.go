package tor

import (
	"github.com/astralp2p/astral-go/api/tor"
	"github.com/astralp2p/astral-go/astral/fmt"
	"github.com/astralp2p/astral-go/astral/log/theme"
)

type EndpointView struct {
	*tor.Endpoint
}

func (v *EndpointView) Render() string {
	n := theme.Tertiary
	b := n.Bri(theme.More)

	return n.Render("tor:") + b.Render(v.Address())
}

func init() {
	fmt.SetView(func(o *tor.Endpoint) fmt.View {
		return &EndpointView{Endpoint: o}
	})
}
