package views

import (
	"github.com/astralp2p/astral-go/astral"
	"github.com/astralp2p/astral-go/astral/fmt"
	"github.com/astralp2p/astral-go/astral/log"
	"github.com/astralp2p/astral-go/astral/log/theme"
	"github.com/astralp2p/astral-go/sig"
)

type EntryView struct {
	*log.Entry
}

// HideOrigin names the origin whose entries print without an origin prefix.
//
// why: mod/log sets this during Load while earlier-loaded modules already log
// from other goroutines, so a bare package var is written and read at once.
var HideOrigin sig.Value[*astral.Identity]

func (v EntryView) Render() string {
	level := fmt.Sprintf("(%v)", v.Level)

	var line = fmt.Sprintf("%v %v ",
		theme.Level.Render(level),
		NewTimeView(&v.Time),
	)

	if showOrigin(v.Origin) {
		line = fmt.Sprintf("[%v] ", v.Origin) + line
	}

	for _, object := range v.Objects {
		line += fmt.Sprint(object)
	}

	return line
}

// showOrigin reports whether an entry from origin prints its origin prefix.
// An origin equal to HideOrigin is hidden, and an unset or zero HideOrigin
// hides every origin.
func showOrigin(origin *astral.Identity) bool {
	hide := HideOrigin.Get()
	if hide == nil {
		return false
	}

	return !origin.IsEqual(hide) && !hide.IsZero()
}

func UseEntryView() {
	fmt.SetView(func(o *log.Entry) fmt.View {
		return EntryView{o}
	})
}
