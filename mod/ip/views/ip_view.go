package ip

import (
	"github.com/astralp2p/astral-go/api/ip"
	"github.com/astralp2p/astral-go/astral/fmt"
	"github.com/astralp2p/astral-go/astral/log/theme"
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
