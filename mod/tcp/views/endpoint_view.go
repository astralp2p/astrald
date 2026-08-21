package tcp

import (
	"github.com/astralp2p/astral-go/api/tcp"
	"github.com/astralp2p/astral-go/astral/fmt"
	"github.com/astralp2p/astral-go/astral/log/theme"
)

type EndpointView struct {
	*tcp.Endpoint
}

func (v *EndpointView) Render() string {
	n := theme.Tertiary
	b := n.Bri(theme.More)

	return n.Render("tcp:") + b.Render(v.Address())
}

func init() {
	fmt.SetView(func(o *tcp.Endpoint) fmt.View {
		return &EndpointView{Endpoint: o}
	})
}
