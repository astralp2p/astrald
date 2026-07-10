package views

import (
	"github.com/astralp2p/astral-go/astral"
	"github.com/astralp2p/astral-go/astral/fmt"
	"github.com/astralp2p/astral-go/astral/log/theme"
)

type ErrorView struct {
	astral.Error
}

func (v ErrorView) Render() string {
	return theme.Error.Render(v.Error.Error())
}

func init() {
	fmt.SetView(func(o *astral.ErrorMessage) fmt.View {
		return ErrorView{Error: o}
	})
}
