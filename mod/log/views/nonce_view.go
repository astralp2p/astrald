package views

import (
	"github.com/cryptopunkscc/astral-go/astral"
	"github.com/cryptopunkscc/astral-go/astral/fmt"
	"github.com/cryptopunkscc/astral-go/astral/log/theme"
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
