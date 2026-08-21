package views

import (
	"github.com/astralp2p/astral-go/astral"
	"github.com/astralp2p/astral-go/astral/fmt"
)

type QueryView struct {
	*astral.Query
}

func (view QueryView) Render() (out string) {
	out = fmt.Sprintf(
		"[%v] %v -> %v:%v",
		&view.Nonce,
		view.Caller,
		view.Target,
		NewQueryStringView(view.QueryString),
	)

	return out
}

func UseQueryView() {
	fmt.SetView(func(o *astral.Query) fmt.View {
		return QueryView{o}
	})
}
