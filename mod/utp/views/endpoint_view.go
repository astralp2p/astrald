package utp

import (
	"github.com/cryptopunkscc/astral-go/astral/fmt"
	"github.com/cryptopunkscc/astral-go/astral/log/theme"
	"github.com/cryptopunkscc/astrald/mod/utp"
)

type EndpointView struct {
	*utp.Endpoint
}

func (v *EndpointView) Render() string {
	n := theme.Tertiary
	b := n.Bri(theme.More)

	return n.Render("utp:") + b.Render(v.Address())
}

func init() {
	fmt.SetView(func(o *utp.Endpoint) fmt.View {
		return &EndpointView{Endpoint: o}
	})
}
