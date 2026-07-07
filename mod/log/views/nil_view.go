package views

import (
	"github.com/cryptopunkscc/astral-go/astral"
	"github.com/cryptopunkscc/astral-go/astral/fmt"
	"github.com/cryptopunkscc/astral-go/astral/log/theme"
)

type NilView struct {
	astral.Nil
}

func (NilView) Render() string {
	t := theme.Nil
	p := t.Bri(theme.Least)
	return p.Render("(") + t.Render("nil") + p.Render(")")
}

func init() {
	fmt.SetView(func(*astral.Nil) fmt.View {
		return &NilView{}
	})
}
