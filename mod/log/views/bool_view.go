package views

import (
	"github.com/astralp2p/astral-go/astral"
	"github.com/astralp2p/astral-go/astral/fmt"
	"github.com/astralp2p/astral-go/astral/log/theme"
)

type BoolView struct {
	*astral.Bool
}

func (b BoolView) Render() string {
	if *b.Bool {
		return theme.True.Render(b.Bool.String())
	}

	return theme.False.Render(b.Bool.String())
}

func init() {
	fmt.SetView(func(o *astral.Bool) fmt.View {
		return &BoolView{Bool: o}
	})
}
