package views

import (
	"github.com/astralp2p/astral-go/astral"
	"github.com/astralp2p/astral-go/astral/fmt"
	"github.com/astralp2p/astral-go/astral/log"
	"github.com/astralp2p/astral-go/astral/log/theme"
)

type EntryView struct {
	*log.Entry
}

var HideOrigin = astral.Anyone

func (v EntryView) Render() string {
	level := fmt.Sprintf("(%v)", v.Level)

	var line = fmt.Sprintf("%v %v ",
		theme.Level.Render(level),
		NewTimeView(&v.Time),
	)

	if HideOrigin == nil || (!v.Origin.IsEqual(HideOrigin) && !HideOrigin.IsZero()) {
		line = fmt.Sprintf("[%v] ", v.Origin) + line
	}

	for _, object := range v.Objects {
		line += fmt.Sprint(object)
	}

	return line
}

func UseEntryView() {
	fmt.SetView(func(o *log.Entry) fmt.View {
		return EntryView{o}
	})
}
