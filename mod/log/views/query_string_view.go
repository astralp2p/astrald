package views

import (
	"sort"

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

	// why: query.Parse returns a map[string]string and a go map ranges in
	// unspecified order; collect and sort so a given query string always logs
	// identically, as RuntimeMapView does.
	var names = make([]string, 0, len(params))
	for name := range params {
		names = append(names, name)
	}
	sort.Strings(names)

	for i, name := range names {
		if i > 0 {
			out += sep.Render("&")
		}
		out += arg.Render(name) + sep.Render("=") + val.Render(params[name])
	}

	return out
}
