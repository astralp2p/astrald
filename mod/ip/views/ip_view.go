package ip

import (
	"github.com/cryptopunkscc/astral-go/astral/fmt"
	"github.com/cryptopunkscc/astral-go/astral/log/theme"
	"github.com/cryptopunkscc/astrald/mod/ip"
)

type IPView struct {
	*ip.IP
}

func (v IPView) Render() string {
	return theme.Tertiary.Render(v.IP.String())
}

func init() {
	fmt.SetView(func(o *ip.IP) fmt.View {
		return &IPView{IP: o}
	})
}
