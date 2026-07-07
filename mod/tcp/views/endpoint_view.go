package tcp

import (
	"github.com/cryptopunkscc/astral-go/astral/fmt"
	"github.com/cryptopunkscc/astral-go/astral/log/theme"
	"github.com/cryptopunkscc/astrald/mod/tcp"
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
