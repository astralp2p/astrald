package views

import (
	"github.com/astralp2p/astral-go/astral/fmt"
	"github.com/astralp2p/astral-go/astral/log"
	"github.com/astralp2p/astral-go/astral/log/styles"
)

type TagView struct {
	*log.Tag
}

func (v TagView) Render() string {
	c := styles.ColorFromString(v.Tag.String())

	return "[" + c.Render(v.Tag.String()) + "] "
}

func init() {
	fmt.SetView(func(o *log.Tag) fmt.View {
		return &TagView{Tag: o}
	})
}
