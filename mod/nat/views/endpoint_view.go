package nat

import (
	"github.com/cryptopunkscc/astral-go/astral/fmt"
	"github.com/cryptopunkscc/astral-go/astral/log/theme"
	"github.com/cryptopunkscc/astrald/mod/nat"
)

type EndpointView struct {
	*nat.Endpoint
}

func (v *EndpointView) Render() string {
	n := theme.Tertiary
	b := n.Bri(theme.More)

	return n.Render("udp:") + b.Render(v.Address())
}

func init() {
	fmt.SetView(func(o *nat.Endpoint) fmt.View {
		return &EndpointView{Endpoint: o}
	})
}
