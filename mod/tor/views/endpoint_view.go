package tor

import (
	"github.com/cryptopunkscc/astral-go/astral/fmt"
	"github.com/cryptopunkscc/astral-go/astral/log/theme"
	"github.com/cryptopunkscc/astrald/mod/tor"
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
