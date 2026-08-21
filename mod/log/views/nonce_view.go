package views

import (
	"github.com/astralp2p/astral-go/astral"
	"github.com/astralp2p/astral-go/astral/fmt"
	"github.com/astralp2p/astral-go/astral/log/theme"
)

type NonceView struct {
	*astral.Nonce
}

func (v NonceView) Render() string {
	return theme.Nonce.Render(v.Nonce.String())
}

func init() {
	fmt.SetView(func(o *astral.Nonce) fmt.View {
		return &NonceView{Nonce: o}
	})
}
