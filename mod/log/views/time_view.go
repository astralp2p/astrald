package views

import (
	"strings"

	"github.com/cryptopunkscc/astral-go/astral"
	"github.com/cryptopunkscc/astral-go/astral/fmt"
	"github.com/cryptopunkscc/astral-go/astral/log/styles"
	"github.com/cryptopunkscc/astral-go/astral/log/theme"
)

const DefaultTimeLayout = "15:04:05.000"
const LongTimeLayout = "2006-01-02 15:04:05"

type TimeView struct {
	*astral.Time
	Layout string
	Color  styles.Color
}

func NewTimeView(time *astral.Time) *TimeView {
	return &TimeView{Time: time, Layout: DefaultTimeLayout, Color: theme.Time}
}

func NewTimeViewColor(time *astral.Time, layout string, color styles.Color) *TimeView {
	return &TimeView{Time: time, Layout: layout, Color: color}
}

func (v TimeView) Render() (out string) {
	layout := v.Layout
	if layout == "" {
		layout = DefaultTimeLayout
	}

	t := v.Time.Time().Format(layout)

	c := v.Color

	if !strings.HasSuffix(layout, ".000") {
		return c.Render(t)
	}

	l := len(t)

	msec := t[l-4:]
	t = t[0 : l-4]

	return c.Render(t) + c.Bri(theme.Less).Render(msec)
}

func init() {
	fmt.SetView(func(o *astral.Time) fmt.View {
		return &TimeView{Time: o, Color: theme.Time}
	})
}
