package views

import (
	"github.com/astralp2p/astral-go/astral"
	"github.com/astralp2p/astral-go/astral/log/theme"
	"github.com/astralp2p/astral-go/lib/query"
)

type QueryStringView struct {
	*astral.String16
}

func NewQueryStringView(str string) QueryStringView {
	return QueryStringView{astral.NewString16(str)}
}

func (view QueryStringView) Render() (out string) {
	op, params := query.Parse(view.String16.String())

	out = theme.Op.Render(op)

	var (
		sep = theme.Normal.Bri(theme.Least)
		val = theme.Normal
		arg = theme.Normal.Bri(theme.More)
	)

	if len(params) > 0 {
		out += sep.Render("?")
	}

	var first = true
	for name, field := range params {
		if !first {
			out += sep.Render("&")
		}
		out += arg.Render(name) + sep.Render("=") + val.Render(field)
		first = false
	}

	return out
}
