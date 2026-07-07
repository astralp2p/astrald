package gateway

import (
	"github.com/cryptopunkscc/astral-go/astral/fmt"
	"github.com/cryptopunkscc/astral-go/astral/log/theme"
	"github.com/cryptopunkscc/astrald/mod/gateway"
)

type EndpointView struct {
	*gateway.Endpoint
}

func (v *EndpointView) Render() string {
	n := theme.Tertiary
	b := n.Bri(theme.More)

	return n.Render("gw:") +
		b.Render(v.GatewayID.String()) +
		n.Render(":") +
		b.Render(v.TargetID.String())
}

func init() {
	fmt.SetView(func(o *gateway.Endpoint) fmt.View {
		return &EndpointView{Endpoint: o}
	})
}
