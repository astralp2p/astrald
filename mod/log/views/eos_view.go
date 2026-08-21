package views

import (
	"github.com/astralp2p/astral-go/astral"
	"github.com/astralp2p/astral-go/astral/fmt"
	"github.com/astralp2p/astral-go/astral/log/theme"
)

type EOSView struct {
	astral.EOS
}

func (EOSView) Render() string {
	t := theme.EOS
	p := t.Bri(theme.Least)
	return p.Render("(") + t.Render("end of stream") + p.Render(")")
}

func init() {
	fmt.SetView(func(*astral.EOS) fmt.View {
		return &EOSView{}
	})
}
