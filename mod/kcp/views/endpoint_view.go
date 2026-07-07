package kcp

import (
	"github.com/cryptopunkscc/astral-go/api/kcp"
	"github.com/cryptopunkscc/astral-go/astral/fmt"
	"github.com/cryptopunkscc/astral-go/astral/log/theme"
)

type EndpointView struct {
	*kcp.Endpoint
}

func (v *EndpointView) Render() string {
	n := theme.Tertiary
	b := n.Bri(theme.More)

	return n.Render("kcp:") + b.Render(v.Address())
}

func init() {
	fmt.SetView(func(o *kcp.Endpoint) fmt.View {
		return &EndpointView{Endpoint: o}
	})
}
